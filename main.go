package main

import (
	"fmt"

	"github.com/ast9501/helm-clientgo/pkg"
)

var args = map[string]string{
	"set": "service.type=NodePort",
}

func main() {
	// Add repo and update
	// Add repo nginx-stable with repoUrl
	err := pkg.RepoAdd("nginx-stable", "https://charts.bitnami.com/bitnami")
	if err != nil {
		fmt.Println(err)
	}
	// Update repo
	err = pkg.RepoUpdate()
	if err != nil {
		fmt.Println(err)
	}

	// Install chart from repo with given namespace and values
	// releaseName nginx in dev namespace, nginx-stable as repo name, nginx is chart name and 13.2.31 is chartVersion; args will used to set values

	err = pkg.InstallChart("nginx", "dev", "nginx-stable", "nginx", "13.2.31", args)
	if err != nil {
		fmt.Println(err)
	}

	// Uninstall chart
	err = pkg.UninstallHelmChart("dev", "nginx")
	if err != nil {
		fmt.Println(err)
	}

}
