# Look up the 4 GB plan (Linux, hourly billing) by its flavor name
data "conohavps_flavor" "plan" {
  name = "g2l-t-c4m4"
}

resource "conohavps_instance" "example" {
  instance_name_tag = "example"
  flavor_id         = data.conohavps_flavor.plan.id
  block_device = [
    {
      uuid = conohavps_volume.boot.id
    }
  ]
}
