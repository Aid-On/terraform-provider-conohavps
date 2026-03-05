resource "conohavps_securitygroup" "group_1" {
  name = "tf-example-security-group-1"
}

resource "conohavps_securitygroup" "group_2" {
  name        = "tf-example-security-group-2"
  description = "this is description example"
}
