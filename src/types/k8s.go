package types

// ZarfState is maintained as a secret in the Zarf namespace to track Zarf init data
type ZarfState struct {
	ZarfAppliance bool         `json:"zarfAppliance" jsonschema:"description=Indicates if Zarf was initialized while deploying its own k8s cluster"`
	Distro        string       `json:"distro" jsonschema:"description=K8s distribution of the cluster Zarf was deployed to"`
	Architecture  string       `json:"architecture" jsonschema:"description=Machine architecture of the k8s node(s)"`
	StorageClass  string       `json:"storageClass" jsonschema:"Default StorageClass value Zarf uses for variable templating"`
	Secret        string       `json:"secret"`
	NodePort      string       `json:"nodePort"`
	AgentTLS      GeneratedPKI `json:"agentTLS" jsonschema:"PKI certificate information for the agent pods Zarf manages"`
}

type DeployedPackage struct {
	Name       string
	Data       ZarfPackage
	CLIVersion string

	DeployedComponents map[string]DeployedComponent
}

type DeployedComponent struct {
	InstalledCharts []InstalledCharts
}

type InstalledCharts struct {
	Namespace string
	ChartName string
}

type GeneratedPKI struct {
	CA   []byte
	Cert []byte
	Key  []byte
}
