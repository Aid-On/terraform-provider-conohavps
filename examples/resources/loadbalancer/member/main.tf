resource "conohavps_lb_loadbalancer" "lb_1" {
  name = "tf-example-lb-1"
}

resource "conohavps_lb_listener" "listener_1" {
  name            = "tf-example-listener-1"
  protocol        = "TCP"
  protocol_port   = 80
  loadbalancer_id = conohavps_lb_loadbalancer.lb_1.id
}

resource "conohavps_lb_pool" "pool_1" {
  name         = "tf-example-pool-1"
  protocol     = "TCP"
  lb_algorithm = "ROUND_ROBIN"
  listener_id  = conohavps_lb_listener.listener_1.id
}

resource "conohavps_lb_member" "member_1" {
  pool_id       = conohavps_lb_pool.pool_1.id
  name          = "tf-example-member-1"
  address       = "203.0.113.10"
  protocol_port = 80
}

resource "conohavps_lb_member" "member_2" {
  pool_id        = conohavps_lb_pool.pool_1.id
  name           = "tf-example-member-2"
  address        = "203.0.113.11"
  protocol_port  = 80
  admin_state_up = false
}
