resource "conohavps_network" "local" {}

resource "conohavps_subnet" "local" {
  network_id = conohavps_network.local.id
  cidr       = "10.0.0.0/24"
}

resource "conohavps_securitygroup" "local" {
  name = "tf-example-local"
}

# A port with a fixed address. Referring to the subnet also makes Terraform
# create the subnet first and delete the port before it.
resource "conohavps_port" "web" {
  network_id = conohavps_network.local.id
  fixed_ips = [
    {
      subnet_id  = conohavps_subnet.local.id
      ip_address = "10.0.0.10"
    }
  ]
  security_group_ids = [conohavps_securitygroup.local.id]

  # A virtual IP shared with another server.
  allowed_address_pairs = [
    {
      ip_address = "10.0.0.100/32"
    }
  ]
}

# A port whose address is assigned automatically.
resource "conohavps_port" "db" {
  network_id = conohavps_network.local.id
  fixed_ips = [
    {
      subnet_id = conohavps_subnet.local.id
    }
  ]
}
