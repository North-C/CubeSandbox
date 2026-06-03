// Copyright 2020, ARM Limited.
//
// SPDX-License-Identifier: Apache-2.0 AND BSD-3-Clause

use super::interrupt_controller::{Error, InterruptController};
extern crate arch;
use anyhow::anyhow;
use arch::layout;
use hypervisor::{
    arch::aarch64::gic::{Vgic, VgicConfig},
    CpuState,
};
use std::collections::HashSet;
use std::result;
use std::sync::{Arc, Mutex};
use std::time::Instant;
use vm_device::interrupt::{
    InterruptIndex, InterruptManager, InterruptSourceConfig, InterruptSourceGroup,
    LegacyIrqSourceConfig, MsiIrqGroupConfig,
};
use vm_memory::address::Address;
use vm_migration::{Migratable, MigratableError, Pausable, Snapshot, Snapshottable, Transportable};
use vmm_sys_util::eventfd::EventFd;

type Result<T> = result::Result<T, Error>;

// Reserve 32 IRQs for legacy devices.
pub const IRQ_LEGACY_BASE: usize = layout::IRQ_BASE as usize;
pub const IRQ_LEGACY_COUNT: usize = 32;

// Gic (Generic Interupt Controller) struct provides all the functionality of a
// GIC device. It wraps a hypervisor-emulated GIC device (Vgic) provided by the
// `hypervisor` crate.
// Gic struct also implements InterruptController to provide interrupt delivery
// service.
pub struct Gic {
    interrupt_source_group: Arc<dyn InterruptSourceGroup>,
    legacy_irqs: Mutex<HashSet<InterruptIndex>>,
    // The hypervisor agnostic virtual GIC
    vgic: Option<Arc<Mutex<dyn Vgic>>>,
}

impl Gic {
    pub fn new(
        _vcpu_count: u8,
        interrupt_manager: Arc<dyn InterruptManager<GroupConfig = MsiIrqGroupConfig>>,
    ) -> Result<Gic> {
        let interrupt_source_group = interrupt_manager
            .create_group(MsiIrqGroupConfig {
                base: IRQ_LEGACY_BASE as InterruptIndex,
                count: IRQ_LEGACY_COUNT as InterruptIndex,
            })
            .map_err(Error::CreateInterruptSourceGroup)?;

        Ok(Gic {
            interrupt_source_group,
            legacy_irqs: Mutex::new(HashSet::new()),
            vgic: None,
        })
    }

    /// Default config implied by arch::layout
    pub fn create_default_config(vcpu_count: u64) -> VgicConfig {
        let redists_size = layout::GIC_V3_REDIST_SIZE * vcpu_count;
        let redists_addr = layout::GIC_V3_DIST_START.raw_value() - redists_size;
        VgicConfig {
            vcpu_count,
            dist_addr: layout::GIC_V3_DIST_START.raw_value(),
            dist_size: layout::GIC_V3_DIST_SIZE,
            redists_addr,
            redists_size,
            msi_addr: redists_addr - layout::GIC_V3_ITS_SIZE,
            msi_size: layout::GIC_V3_ITS_SIZE,
            nr_irqs: layout::IRQ_NUM,
        }
    }

    pub fn create_vgic(
        &mut self,
        vm: &Arc<dyn hypervisor::Vm>,
        config: VgicConfig,
    ) -> Result<Arc<Mutex<dyn Vgic>>> {
        let vgic = vm.create_vgic(config).map_err(Error::CreateGic)?;
        self.vgic = Some(vgic.clone());
        Ok(vgic.clone())
    }

    pub fn set_gicr_typers(&mut self, vcpu_states: &[CpuState]) {
        let vgic = self.vgic.as_ref().unwrap().clone();
        vgic.lock().unwrap().set_gicr_typers(vcpu_states);
    }
}

impl InterruptController for Gic {
    fn register_legacy_irq(&mut self, irq: usize) -> Result<()> {
        self.legacy_irqs
            .lock()
            .unwrap()
            .insert(irq as InterruptIndex);
        Ok(())
    }

    fn enable(&self) -> Result<()> {
        let timing_start = Instant::now();
        let mut irqs: Vec<InterruptIndex> =
            self.legacy_irqs.lock().unwrap().iter().copied().collect();
        irqs.sort_unstable();

        // Set irqfd for legacy interrupts
        let stage_start = Instant::now();
        if !irqs.is_empty() {
            self.interrupt_source_group
                .enable_selected(&irqs)
                .map_err(Error::EnableInterrupt)?;
        }
        let legacy_enable_ms = stage_start.elapsed().as_secs_f64() * 1000.0;

        // Set irq_routing for legacy interrupts.
        //   irqchip: Hardcode to 0 as we support only 1 GIC
        //   pin: Use irq number as pin
        let stage_start = Instant::now();
        let configs: Vec<_> = irqs
            .iter()
            .map(|i| {
                let config = LegacyIrqSourceConfig {
                    irqchip: 0,
                    pin: (*i as usize - IRQ_LEGACY_BASE) as u32,
                };
                (*i, InterruptSourceConfig::LegacyIrq(config), false)
            })
            .collect();
        if !configs.is_empty() {
            self.interrupt_source_group
                .update_many(&configs)
                .map_err(Error::EnableInterrupt)?;
        }
        let legacy_route_update_ms = stage_start.elapsed().as_secs_f64() * 1000.0;

        log::info!(
            "restore timing stage=gic_enable_detail total_ms={:.3} legacy_irq_count={} legacy_enable_ms={:.3} legacy_route_update_ms={:.3}",
            timing_start.elapsed().as_secs_f64() * 1000.0,
            irqs.len(),
            legacy_enable_ms,
            legacy_route_update_ms
        );

        Ok(())
    }

    // This should be called anytime an interrupt needs to be injected into the
    // running guest.
    fn service_irq(&mut self, irq: usize) -> Result<()> {
        self.interrupt_source_group
            .trigger(irq as InterruptIndex)
            .map_err(Error::TriggerInterrupt)?;

        Ok(())
    }

    fn notifier(&self, irq: usize) -> Option<EventFd> {
        self.interrupt_source_group.notifier(irq as InterruptIndex)
    }
}

pub const GIC_V3_ITS_SNAPSHOT_ID: &str = "gic-v3-its";
impl Snapshottable for Gic {
    fn id(&self) -> String {
        GIC_V3_ITS_SNAPSHOT_ID.to_string()
    }

    fn snapshot(&mut self) -> std::result::Result<Snapshot, MigratableError> {
        let vgic = self.vgic.as_ref().unwrap().clone();
        let state = vgic.lock().unwrap().state().unwrap();
        Snapshot::new_from_state(&self.id(), &state)
    }

    fn restore(&mut self, snapshot: Snapshot) -> std::result::Result<(), MigratableError> {
        let vgic = self.vgic.as_ref().unwrap().clone();
        vgic.lock()
            .unwrap()
            .set_state(&snapshot.to_state(&self.id())?)
            .map_err(|e| {
                MigratableError::Restore(anyhow!("Could not restore GICv3ITS state {:?}", e))
            })?;
        Ok(())
    }
}

impl Pausable for Gic {
    fn pause(&mut self) -> std::result::Result<(), MigratableError> {
        // Flush tables to guest RAM
        let vgic = self.vgic.as_ref().unwrap().clone();
        vgic.lock().unwrap().save_data_tables().map_err(|e| {
            MigratableError::Pause(anyhow!(
                "Could not save GICv3ITS GIC pending tables {:?}",
                e
            ))
        })?;
        Ok(())
    }
}
impl Transportable for Gic {}
impl Migratable for Gic {}
