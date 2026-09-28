# Look up the 300 Mbps policy (a paid option) by its name
data "conohavps_qos_policy" "fast" {
  name = "global-i_300000-o_300000"
}

resource "conohavps_additional_ip" "fast" {
  ip_count      = 1
  qos_policy_id = data.conohavps_qos_policy.fast.id
}
