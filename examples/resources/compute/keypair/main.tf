# keypair creation basic
resource "conohavps_keypair" "test" {
  name = "keypair_1" # keypair name
}
# keypair creation with provided public key
resource "conohavps_keypair" "with_public_key" {
  name = "keypair_2" # keypair name
  public_key = "ssh-rsa xxxxxxxxxxxxxxxxxxxxxxxxxxx..." # Replace with your actual public key
}