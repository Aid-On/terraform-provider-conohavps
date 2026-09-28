resource "conohavps_dns_domain" "example" {
  name        = "example.com."
  ttl         = 3600
  email       = "hostmaster@example.com"
}

resource "conohavps_dns_record" "www" {
  domain_id   = conohavps_dns_domain.example.id
  name        = "www.example.com."
  type        = "A"
  data        = "192.0.2.10"
  ttl         = 300
}

resource "conohavps_dns_record" "mx" {
  domain_id = conohavps_dns_domain.example.id
  name      = "example.com."
  type      = "MX"
  data      = "mail.example.com."
  priority  = 10
}

resource "conohavps_dns_record" "sip" {
  domain_id = conohavps_dns_domain.example.id
  name      = "_sip._tcp.example.com."
  type      = "SRV"
  data      = "sip.example.com."
  priority  = 10
  weight    = 60
  port      = 5060
}
