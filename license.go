package goflexlm

// LicenseFile is the supported FlexLM license document.
type LicenseFile struct {
	Servers   []LicenseServer  `json:"servers"`
	Vendors   []LicenseVendor  `json:"vendors"`
	Features  []LicenseFeature `json:"features"`
	UseServer bool             `json:"use_server"`
}

// LicenseServer is a SERVER record in a license document.
type LicenseServer struct {
	Line       int                `json:"line"`
	Host       string             `json:"host"`
	HostID     string             `json:"host_id"`
	Port       *int               `json:"port,omitempty"`
	Attributes []LicenseAttribute `json:"attributes,omitempty"`
}

// LicenseVendor is a VENDOR record in a license document.
type LicenseVendor struct {
	Line       int                `json:"line"`
	Name       string             `json:"name"`
	DaemonPath string             `json:"daemon_path,omitempty"`
	Attributes []LicenseAttribute `json:"attributes,omitempty"`
}

// LicenseFeatureKind identifies a FEATURE or INCREMENT record.
type LicenseFeatureKind string

// Supported FlexLM capacity-record kinds.
const (
	LicenseFeatureLine   LicenseFeatureKind = "FEATURE"
	LicenseIncrementLine LicenseFeatureKind = "INCREMENT"
)

// LicenseFeature is a FEATURE or INCREMENT record in a license document.
type LicenseFeature struct {
	Line       int                `json:"line"`
	Kind       LicenseFeatureKind `json:"kind"`
	Name       string             `json:"name"`
	Vendor     string             `json:"vendor"`
	Version    string             `json:"version"`
	Expiration string             `json:"expiration"`
	Licenses   *int               `json:"licenses"`
	Uncounted  bool               `json:"uncounted"`
	Attributes []LicenseAttribute `json:"attributes,omitempty"`
}

// LicenseAttribute preserves one ordered license-record attribute.
type LicenseAttribute struct {
	Name     string `json:"name"`
	Value    string `json:"value,omitempty"`
	HasValue bool   `json:"has_value"`
}
