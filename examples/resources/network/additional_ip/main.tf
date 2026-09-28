# Two additional IP addresses on one port, with the default security group.
resource "conohavps_additional_ip" "extra" {
  ip_count = 2
}

# 300 Mbps bandwidth (a paid option) for the additional IP addresses.
data "conohavps_qos_policy" "fast" {
  name = "global-i_300000-o_300000"
}

resource "conohavps_additional_ip" "fast" {
  ip_count      = 1
  qos_policy_id = data.conohavps_qos_policy.fast.id
}
