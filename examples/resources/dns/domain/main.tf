resource "conohavps_dns_domain" "example" {
  name        = "example.com."
  ttl         = 3600
  email       = "hostmaster@example.com"
  description = "Company website"
}
