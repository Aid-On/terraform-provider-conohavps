resource "conohavps_network" "local" {}

resource "conohavps_subnet" "local" {
  network_id = conohavps_network.local.id
  cidr       = "10.0.0.0/24"
}

resource "conohavps_port" "web" {
  network_id = conohavps_network.local.id
  fixed_ips = [
    {
      subnet_id  = conohavps_subnet.local.id
      ip_address = "10.0.0.10"
    }
  ]
}

resource "conohavps_additional_ip" "extra" {
  ip_count = 1
}

# conohavps_instance.web is defined elsewhere.
resource "conohavps_port_attachment" "web" {
  server_id = conohavps_instance.web.id
  port_id   = conohavps_port.web.id
}

resource "conohavps_port_attachment" "extra" {
  server_id = conohavps_instance.web.id
  port_id   = conohavps_additional_ip.extra.id
}
