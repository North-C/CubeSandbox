// Copyright © 2019 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0 AND BSD-3-Clause
//

use devices::interrupt_controller::InterruptController;
use hypervisor::IrqRoutingEntry;
use std::collections::HashMap;
use std::io;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};
use std::time::Instant;
use vm_allocator::SystemAllocator;
use vm_device::interrupt::{
    InterruptIndex, InterruptManager, InterruptSourceConfig, InterruptSourceGroup,
    LegacyIrqGroupConfig, MsiIrqGroupConfig,
};
use vmm_sys_util::eventfd::EventFd;

/// Reuse std::io::Result to simplify interoperability among crates.
pub type Result<T> = std::io::Result<T>;

struct InterruptRoute {
    gsi: u32,
    irq_fd: EventFd,
    registered: AtomicBool,
}

impl InterruptRoute {
    pub fn new(allocator: &mut SystemAllocator) -> Result<Self> {
        let irq_fd = EventFd::new(libc::EFD_NONBLOCK)?;
        let gsi = allocator
            .allocate_gsi()
            .ok_or_else(|| io::Error::new(io::ErrorKind::Other, "Failed allocating new GSI"))?;

        Ok(InterruptRoute {
            gsi,
            irq_fd,
            registered: AtomicBool::new(false),
        })
    }

    pub fn enable(&self, vm: &Arc<dyn hypervisor::Vm>) -> Result<bool> {
        if !self.registered.load(Ordering::Acquire) {
            vm.register_irqfd(&self.irq_fd, self.gsi).map_err(|e| {
                io::Error::new(
                    io::ErrorKind::Other,
                    format!("Failed registering irq_fd: {}", e),
                )
            })?;

            // Update internals to track the irq_fd as "registered".
            self.registered.store(true, Ordering::Release);

            return Ok(true);
        }

        Ok(false)
    }

    pub fn disable(&self, vm: &Arc<dyn hypervisor::Vm>) -> Result<()> {
        if self.registered.load(Ordering::Acquire) {
            vm.unregister_irqfd(&self.irq_fd, self.gsi).map_err(|e| {
                io::Error::new(
                    io::ErrorKind::Other,
                    format!("Failed unregistering irq_fd: {}", e),
                )
            })?;

            // Update internals to track the irq_fd as "unregistered".
            self.registered.store(false, Ordering::Release);
        }

        Ok(())
    }

    pub fn trigger(&self) -> Result<()> {
        self.irq_fd.write(1)
    }

    pub fn notifier(&self) -> Option<EventFd> {
        Some(
            self.irq_fd
                .try_clone()
                .expect("Failed cloning interrupt's EventFd"),
        )
    }
}

pub struct RoutingEntry {
    route: IrqRoutingEntry,
    masked: bool,
}

pub struct MsiInterruptGroup {
    vm: Arc<dyn hypervisor::Vm>,
    gsi_msi_routes: Arc<Mutex<HashMap<u32, RoutingEntry>>>,
    irq_routes: HashMap<InterruptIndex, InterruptRoute>,
}

impl MsiInterruptGroup {
    fn set_gsi_routes(&self, routes: &HashMap<u32, RoutingEntry>) -> Result<()> {
        let mut entry_vec: Vec<IrqRoutingEntry> = Vec::new();
        for (_, entry) in routes.iter() {
            if entry.masked {
                continue;
            }

            entry_vec.push(entry.route);
        }

        self.vm.set_gsi_routing(&entry_vec).map_err(|e| {
            io::Error::new(
                io::ErrorKind::Other,
                format!("Failed setting GSI routing: {}", e),
            )
        })
    }
}

impl MsiInterruptGroup {
    fn new(
        vm: Arc<dyn hypervisor::Vm>,
        gsi_msi_routes: Arc<Mutex<HashMap<u32, RoutingEntry>>>,
        irq_routes: HashMap<InterruptIndex, InterruptRoute>,
    ) -> Self {
        MsiInterruptGroup {
            vm,
            gsi_msi_routes,
            irq_routes,
        }
    }
}

impl InterruptSourceGroup for MsiInterruptGroup {
    fn enable(&self) -> Result<()> {
        let timing_start = Instant::now();
        let mut route_count = 0;
        let mut new_registered = 0;
        let mut already_registered = 0;
        let mut register_irqfd_ms = 0.0;
        let mut max_route_ms = 0.0;

        for (_, route) in self.irq_routes.iter() {
            route_count += 1;
            let route_start = Instant::now();
            let registered = route.enable(&self.vm)?;
            let route_ms = route_start.elapsed().as_secs_f64() * 1000.0;
            max_route_ms = f64::max(max_route_ms, route_ms);
            if registered {
                new_registered += 1;
                register_irqfd_ms += route_ms;
            } else {
                already_registered += 1;
            }
        }

        log::info!(
            "restore timing stage=interrupt_group_enable total_ms={:.3} route_count={} new_registered={} already_registered={} register_irqfd_ms={:.3} max_route_ms={:.3}",
            timing_start.elapsed().as_secs_f64() * 1000.0,
            route_count,
            new_registered,
            already_registered,
            register_irqfd_ms,
            max_route_ms
        );

        Ok(())
    }

    fn enable_selected(&self, indexes: &[InterruptIndex]) -> Result<()> {
        let timing_start = Instant::now();
        let mut route_count = 0;
        let mut new_registered = 0;
        let mut already_registered = 0;
        let mut missing_routes = 0;
        let mut register_irqfd_ms = 0.0;
        let mut max_route_ms = 0.0;

        for index in indexes {
            if let Some(route) = self.irq_routes.get(index) {
                route_count += 1;
                let route_start = Instant::now();
                let registered = route.enable(&self.vm)?;
                let route_ms = route_start.elapsed().as_secs_f64() * 1000.0;
                max_route_ms = f64::max(max_route_ms, route_ms);
                if registered {
                    new_registered += 1;
                    register_irqfd_ms += route_ms;
                } else {
                    already_registered += 1;
                }
            } else {
                missing_routes += 1;
            }
        }

        log::info!(
            "restore timing stage=interrupt_group_enable_selected total_ms={:.3} requested_count={} route_count={} new_registered={} already_registered={} missing_routes={} register_irqfd_ms={:.3} max_route_ms={:.3}",
            timing_start.elapsed().as_secs_f64() * 1000.0,
            indexes.len(),
            route_count,
            new_registered,
            already_registered,
            missing_routes,
            register_irqfd_ms,
            max_route_ms
        );

        if missing_routes > 0 {
            return Err(io::Error::new(
                io::ErrorKind::Other,
                format!(
                    "enable_selected: {} invalid interrupt indexes",
                    missing_routes
                ),
            ));
        }

        Ok(())
    }

    fn disable(&self) -> Result<()> {
        for (_, route) in self.irq_routes.iter() {
            route.disable(&self.vm)?;
        }

        Ok(())
    }

    fn trigger(&self, index: InterruptIndex) -> Result<()> {
        if let Some(route) = self.irq_routes.get(&index) {
            return route.trigger();
        }

        Err(io::Error::new(
            io::ErrorKind::Other,
            format!("trigger: Invalid interrupt index {}", index),
        ))
    }

    fn notifier(&self, index: InterruptIndex) -> Option<EventFd> {
        if let Some(route) = self.irq_routes.get(&index) {
            return route.notifier();
        }

        None
    }

    fn update(
        &self,
        index: InterruptIndex,
        config: InterruptSourceConfig,
        masked: bool,
    ) -> Result<()> {
        if let Some(route) = self.irq_routes.get(&index) {
            let entry = RoutingEntry {
                route: self.vm.make_routing_entry(route.gsi, &config),
                masked,
            };
            if masked {
                route.disable(&self.vm)?;
            } else {
                let _ = route.enable(&self.vm)?;
            }
            let mut routes = self.gsi_msi_routes.lock().unwrap();
            routes.insert(route.gsi, entry);
            return self.set_gsi_routes(&routes);
        }

        Err(io::Error::new(
            io::ErrorKind::Other,
            format!("update: Invalid interrupt index {}", index),
        ))
    }

    fn update_many(&self, configs: &[(InterruptIndex, InterruptSourceConfig, bool)]) -> Result<()> {
        let timing_start = Instant::now();
        let mut new_registered = 0;
        let mut already_registered = 0;
        let mut disabled = 0;
        let mut register_irqfd_ms = 0.0;
        let mut max_route_ms = 0.0;
        let mut updates = Vec::with_capacity(configs.len());

        for (index, config, masked) in configs {
            if let Some(route) = self.irq_routes.get(index) {
                let entry = RoutingEntry {
                    route: self.vm.make_routing_entry(route.gsi, config),
                    masked: *masked,
                };
                if *masked {
                    route.disable(&self.vm)?;
                    disabled += 1;
                } else {
                    let route_start = Instant::now();
                    let registered = route.enable(&self.vm)?;
                    let route_ms = route_start.elapsed().as_secs_f64() * 1000.0;
                    max_route_ms = f64::max(max_route_ms, route_ms);
                    if registered {
                        new_registered += 1;
                        register_irqfd_ms += route_ms;
                    } else {
                        already_registered += 1;
                    }
                }
                updates.push((route.gsi, entry));
            } else {
                return Err(io::Error::new(
                    io::ErrorKind::Other,
                    format!("update_many: Invalid interrupt index {}", index),
                ));
            }
        }

        let set_gsi_routes_start = Instant::now();
        let mut routes = self.gsi_msi_routes.lock().unwrap();
        for (gsi, entry) in updates {
            routes.insert(gsi, entry);
        }
        self.set_gsi_routes(&routes)?;
        let set_gsi_routes_ms = set_gsi_routes_start.elapsed().as_secs_f64() * 1000.0;

        log::info!(
            "restore timing stage=interrupt_group_update_many total_ms={:.3} config_count={} new_registered={} already_registered={} disabled={} register_irqfd_ms={:.3} max_route_ms={:.3} set_gsi_routes_ms={:.3}",
            timing_start.elapsed().as_secs_f64() * 1000.0,
            configs.len(),
            new_registered,
            already_registered,
            disabled,
            register_irqfd_ms,
            max_route_ms,
            set_gsi_routes_ms
        );

        Ok(())
    }
}

pub struct LegacyUserspaceInterruptGroup {
    ioapic: Arc<Mutex<dyn InterruptController>>,
    irq: u32,
}

impl LegacyUserspaceInterruptGroup {
    fn new(ioapic: Arc<Mutex<dyn InterruptController>>, irq: u32) -> Self {
        LegacyUserspaceInterruptGroup { ioapic, irq }
    }
}

impl InterruptSourceGroup for LegacyUserspaceInterruptGroup {
    fn trigger(&self, _index: InterruptIndex) -> Result<()> {
        self.ioapic
            .lock()
            .unwrap()
            .service_irq(self.irq as usize)
            .map_err(|e| {
                io::Error::new(
                    io::ErrorKind::Other,
                    format!("failed to inject IRQ #{}: {:?}", self.irq, e),
                )
            })
    }

    fn update(
        &self,
        _index: InterruptIndex,
        _config: InterruptSourceConfig,
        _masked: bool,
    ) -> Result<()> {
        Ok(())
    }

    fn notifier(&self, _index: InterruptIndex) -> Option<EventFd> {
        self.ioapic.lock().unwrap().notifier(self.irq as usize)
    }
}

pub struct LegacyUserspaceInterruptManager {
    ioapic: Arc<Mutex<dyn InterruptController>>,
}

pub struct MsiInterruptManager {
    allocator: Arc<Mutex<SystemAllocator>>,
    vm: Arc<dyn hypervisor::Vm>,
    gsi_msi_routes: Arc<Mutex<HashMap<u32, RoutingEntry>>>,
}

impl LegacyUserspaceInterruptManager {
    pub fn new(ioapic: Arc<Mutex<dyn InterruptController>>) -> Self {
        LegacyUserspaceInterruptManager { ioapic }
    }
}

impl MsiInterruptManager {
    pub fn new(allocator: Arc<Mutex<SystemAllocator>>, vm: Arc<dyn hypervisor::Vm>) -> Self {
        // Create a shared list of GSI that can be shared through all PCI
        // devices. This way, we can maintain the full list of used GSI,
        // preventing one device from overriding interrupts setting from
        // another one.
        let gsi_msi_routes = Arc::new(Mutex::new(HashMap::new()));

        MsiInterruptManager {
            allocator,
            vm,
            gsi_msi_routes,
        }
    }
}

impl InterruptManager for LegacyUserspaceInterruptManager {
    type GroupConfig = LegacyIrqGroupConfig;

    fn create_group(&self, config: Self::GroupConfig) -> Result<Arc<dyn InterruptSourceGroup>> {
        self.ioapic
            .lock()
            .unwrap()
            .register_legacy_irq(config.irq as usize)
            .map_err(|e| {
                io::Error::new(
                    io::ErrorKind::Other,
                    format!("failed to register legacy IRQ #{}: {:?}", config.irq, e),
                )
            })?;

        Ok(Arc::new(LegacyUserspaceInterruptGroup::new(
            self.ioapic.clone(),
            config.irq,
        )))
    }

    fn destroy_group(&self, _group: Arc<dyn InterruptSourceGroup>) -> Result<()> {
        Ok(())
    }
}

impl InterruptManager for MsiInterruptManager {
    type GroupConfig = MsiIrqGroupConfig;

    fn create_group(&self, config: Self::GroupConfig) -> Result<Arc<dyn InterruptSourceGroup>> {
        let mut allocator = self.allocator.lock().unwrap();
        let mut irq_routes: HashMap<InterruptIndex, InterruptRoute> =
            HashMap::with_capacity(config.count as usize);
        for i in config.base..config.base + config.count {
            irq_routes.insert(i, InterruptRoute::new(&mut allocator)?);
        }

        Ok(Arc::new(MsiInterruptGroup::new(
            self.vm.clone(),
            self.gsi_msi_routes.clone(),
            irq_routes,
        )))
    }

    fn destroy_group(&self, _group: Arc<dyn InterruptSourceGroup>) -> Result<()> {
        Ok(())
    }
}
