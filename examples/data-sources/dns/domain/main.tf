# Look up a domain already registered in ConoHa DNS by its name
data "conohavps_dns_domain" "example" {
  name = "example.com."
}

resource "conohavps_dns_record" "www" {
  domain_id = data.conohavps_dns_domain.example.id
  name      = "www.example.com."
  type      = "A"
  data      = "192.0.2.10"
}
