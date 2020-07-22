provider "digitalocean" {
  version = "~> 1.13"
}

provider "http" {
  version = "~> 1.1"
}

provider "local" {
  version = "~> 1.3"
}

provider "null" {
  version = "~> 2.1"
}

provider "random" {
  version = "~> 2.2"
}

data "digitalocean_kubernetes_versions" "available" {
  version_prefix = "1.17."
}

resource "digitalocean_kubernetes_cluster" "rack" {
  name    = var.name
  region  = var.region
  version = data.digitalocean_kubernetes_versions.available.latest_version

  node_pool {
    name       = "${var.name}-node"
    size       = var.node_type
    auto_scale = true
    min_nodes  = 2
    max_nodes  = 10
  }
}

# new tokens sometimes take a few seconds to start working
resource "null_resource" "delay_token" {
  provisioner "local-exec" {
    command = "sleep 30"
  }
  triggers = {
    token = digitalocean_kubernetes_cluster.rack.kube_config[0].token
  }
}

resource "local_file" "kubeconfig" {
  depends_on = [digitalocean_kubernetes_cluster.rack, null_resource.delay_token]

  filename = pathexpand("~/.kube/config.do.${var.name}")
  content = templatefile("${path.module}/kubeconfig.tpl", {
    ca       = digitalocean_kubernetes_cluster.rack.kube_config[0].cluster_ca_certificate
    endpoint = digitalocean_kubernetes_cluster.rack.endpoint
    token    = digitalocean_kubernetes_cluster.rack.kube_config[0].token
  })
}
