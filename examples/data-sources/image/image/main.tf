# Look up the Ubuntu 24.04 image by name
data "conohavps_image" "ubuntu" {
  name = "vmi-ubuntu-24.04-amd64"
}

resource "conohavps_volume" "boot" {
  name        = "example-boot"
  size        = 100
  volume_type = "c3j1-ds02-boot"
  image_ref   = data.conohavps_image.ubuntu.id
}
