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

BREAKING CHANGES:

* `conohavps_objectstorage_container`: `web_publishing` is replaced by `container_read` and `container_write`, which hold the full ACL strings
* `conohavps_image_quota`: the size must be 550 GB or more (50 GB plus a multiple of 500), as the API requires

BUG FIXES (checked against the official OpenAPI spec):

* Load balancer resources accept 202 on update; updates used to fail on the real API
* Pool reads no longer decode `members` as objects (the API returns an array of IDs)
* `conohavps_image` decodes timestamps without a time zone and follows pagination
* Identity, DNS and object storage resources accept exactly the status codes the spec lists

DRIFT DETECTION:

* `conohavps_instance_autobackup` reads `backup_status` from server metadata; retention changes in place (retention itself cannot be read from the API)
* `conohavps_subuser` checks the stored password by issuing a token as the sub-user, when the sub-user has the `gmo-identity` role
* `conohavps_port` separates a removed QoS policy from a missing field and exposes `qos_network_policy_id`
* `conohavps_dns_domain` reads the email address back, and domains and records gain `description`; record `ttl` and `type` change in place
* `conohavps_objectstorage_container` reads ACLs, web settings, versioning and `metadata` back from response headers

## 0.1.0 (Unreleased)

FEATURES:
