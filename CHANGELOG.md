## Unreleased (Aid-On fork)

FEATURES:

* **New Data Source:** `conohavps_flavor` looks up a flavor (server plan) by name, such as `g2l-t-c4m4`
* **New Data Source:** `conohavps_image` looks up an active image by name, such as `vmi-ubuntu-24.04-amd64`
* **New Data Source:** `conohavps_backups`, `conohavps_qos_policy`, `conohavps_dns_domain`, `conohavps_image_usage`, `conohavps_permissions`
* **New Resource:** `conohavps_volume_attachment`, `conohavps_volume_snapshot`, `conohavps_instance_autobackup`
* **New Resource:** `conohavps_network`, `conohavps_subnet`, `conohavps_port`, `conohavps_additional_ip`, `conohavps_port_attachment`
* **New Resource:** `conohavps_lb_loadbalancer`, `conohavps_lb_listener`, `conohavps_lb_pool`, `conohavps_lb_member`, `conohavps_lb_health_monitor`
* **New Resource:** `conohavps_dns_domain`, `conohavps_dns_record`
* **New Resource:** `conohavps_objectstorage_container`, `conohavps_objectstorage_quota`, `conohavps_image_quota`
* **New Resource:** `conohavps_role`, `conohavps_subuser`, `conohavps_credential`

IMPROVEMENTS:

* The provider builds clients for the load balancer, object storage, DNS and identity services from the service catalog; a service missing from the catalog fails only the resources that use it
* Acceptance-test setup authenticates against the real API only when `TF_ACC` is set, so unit tests against a fake API run without credentials

## 0.1.0 (Unreleased)

FEATURES:
