package main

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"

	lxd "github.com/canonical/lxd/client"
	"github.com/canonical/lxd/shared/api"
	vault "github.com/hashicorp/vault/api"
)

type InstanceData struct {
	Instances []api.InstanceFull
	Error     error
}

func main() {
	http.HandleFunc("/", statusHandler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	fmt.Printf("Server starting on port %s...\n", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func statusHandler(w http.ResponseWriter, r *http.Request) {
	var data InstanceData

	cert, key, err := getSecrets()
	if err != nil {
		data.Error = fmt.Errorf("Vault Error: %w", err)
		renderTemplate(w, data)
		return
	}

	lxdAddr := os.Getenv("LXD_ADDR")
	args := &lxd.ConnectionArgs{
		TLSClientCert:      cert,
		TLSClientKey:       key,
		InsecureSkipVerify: true,
	}

	client, err := lxd.ConnectLXD(lxdAddr, args)
	if err != nil {
		data.Error = fmt.Errorf("LXD Connection Error: %w", err)
		renderTemplate(w, data)
		return
	}

	// Connect to the 'default' project
	client = client.UseProject("default")

	// FIX: Use InstanceType field name inside the Args struct
	instances, err := client.GetInstancesFull(lxd.GetInstancesFullArgs{
		InstanceType: api.InstanceTypeAny,
	})

	if err != nil {
		data.Error = fmt.Errorf("LXD Query Error: %w", err)
		renderTemplate(w, data)
		return
	}

	data.Instances = instances
	renderTemplate(w, data)
}

func renderTemplate(w http.ResponseWriter, data InstanceData) {
	tmpl, err := template.ParseFiles("index.html")
	if err != nil {
		http.Error(w, "Template Load Error", http.StatusInternalServerError)
		return
	}
	_ = tmpl.Execute(w, data)
}

func getSecrets() (cert, key string, err error) {
	config := vault.DefaultConfig()
	config.Address = os.Getenv("VAULT_ADDR")

	if os.Getenv("VAULT_SKIP_VERIFY") == "true" {
		_ = config.ConfigureTLS(&vault.TLSConfig{Insecure: true})
	}

	client, err := vault.NewClient(config)
	if err != nil {
		return "", "", err
	}

	loginData := map[string]interface{}{
		"role_id":   os.Getenv("VAULT_ROLE_ID"),
		"secret_id": os.Getenv("VAULT_SECRET_ID"),
	}
	resp, err := client.Logical().Write("auth/approle/login", loginData)
	if err != nil || resp == nil {
		return "", "", fmt.Errorf("Vault login failed")
	}
	client.SetToken(resp.Auth.ClientToken)

	secret, err := client.Logical().Read("homelab/data/lxd")
	if err != nil || secret == nil {
		return "", "", fmt.Errorf("failed to read secrets")
	}

	secretData := secret.Data["data"].(map[string]interface{})
	return secretData["client_cert"].(string), secretData["client_key"].(string), nil
}
