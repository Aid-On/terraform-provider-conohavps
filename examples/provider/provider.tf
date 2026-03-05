terraform {
  required_providers {
    conohavps = {
      source = "gmo-internet/conohavps"
    }
  }
}

# Configure the ConoHa VPS provider
provider "conohavps" {
  identity_endpoint = "Your identity endpoint"
  password          = "Your password"
  region            = "Your region"
  tenant_id         = "Your tenant ID"
  user_id           = "Your user ID"
}
