resource "conohavps_network" "local" {}

# A private IPv4 network from /21 to /27.
resource "conohavps_subnet" "local" {
  network_id = conohavps_network.local.id
  cidr       = "10.0.0.0/24"
}
