package main

import "github.com/ast9501/helm-clientgo/pkg"

var args = map[string]string{
	"set": "service.type=NodePort",
}

func main() {
	// Add repo and update
	// Add repo nginx-stable with repoUrl
	pkg.RepoAdd("nginx-stable", "https://charts.bitnami.com/bitnami")
	// Update repo
	pkg.RepoUpdate()

	// Install chart from repo with given namespace and values
	// releaseName nginx in dev namespace, nginx-stable as repo name, nginx is chart name and 13.2.31 is chartVersion; args will used to set values
	pkg.InstallChart("nginx", "dev", "nginx-stable", "nginx", "13.2.31", args)

	// Uninstall chart
	pkg.UninstallHelmChart("dev", "nginx")

}
