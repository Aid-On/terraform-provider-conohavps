resource "conohavps_securitygroup" "group_1" {
  name = "tf-example-security-group-1"
}

resource "conohavps_securitygroup" "group_2" {
  name = "tf-example-security-group-2"
}

# Expose all ports (1-65535)
resource "conohavps_securitygroup_rule" "rule_1" {
  securitygroup_id = resource.conohavps_securitygroup.group_1.id
  direction        = "ingress"
  ethertype        = "IPv4"
  protocol         = "tcp"
  port_range_min   = 0
  port_range_max   = 0
}

# Expose a specific port range
resource "conohavps_securitygroup_rule" "rule_2" {
  securitygroup_id = resource.conohavps_securitygroup.group_2.id
  direction        = "ingress"
  ethertype        = "IPv4"
  protocol         = "tcp"
  port_range_min   = 80
  port_range_max   = 8080
}
