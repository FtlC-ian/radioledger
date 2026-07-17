package signer

import (
	"crypto/x509"
	"fmt"
	"time"
)

// arrlProductionCAPEM is the ARRL LoTW Production CA (intermediate) certificate.
// Extracted from a real ARRL-issued .p12 file (public certificate, not secret).
// Valid: 2023-06-29 → 2027-06-29
const arrlProductionCAPEM = `-----BEGIN CERTIFICATE-----
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
tBwPJo4njjGW
-----END CERTIFICATE-----`

// arrlRootCAPEM is the ARRL LoTW Root CA certificate (self-signed trust anchor).
// Valid: 2023-06-28 → 2033-06-25
const arrlRootCAPEM = `-----BEGIN CERTIFICATE-----
MIIHZzCCBU+gAwIBAgIJALbcCREWTZxRMA0GCSqGSIb3DQEBDQUAMIHSMQswCQYD
VQQGEwJVUzELMAkGA1UECBMCQ1QxEjAQBgNVBAcTCU5ld2luZ3RvbjEkMCIGA1UE
ChMbQW1lcmljYW4gUmFkaW8gUmVsYXkgTGVhZ3VlMR0wGwYDVQQLExRMb2dib29r
IG9mIHRoZSBXb3JsZDElMCMGA1UEAxMcTG9nYm9vayBvZiB0aGUgV29ybGQgUm9v
dCBDQTEYMBYGCgmSJomT8ixkARkWCGFycmwub3JnMRwwGgYJKoZIhvcNAQkBFg1s
b3R3QGFycmwub3JnMB4XDTIzMDYyODEyMjgzNVoXDTMzMDYyNTEyMjgzNVowgdIx
CzAJBgNVBAYTAlVTMQswCQYDVQQIEwJDVDESMBAGA1UEBxMJTmV3aW5ndG9uMSQw
IgYDVQQKExtBbWVyaWNhbiBSYWRpbyBSZWxheSBMZWFndWUxHTAbBgNVBAsTFExv
Z2Jvb2sgb2YgdGhlIFdvcmxkMSUwIwYDVQQDExxMb2dib29rIG9mIHRoZSBXb3Js
ZCBSb290IENBMRgwFgYKCZImiZPyLGQBGRYIYXJybC5vcmcxHDAaBgkqhkiG9w0B
CQEWDWxvdHdAYXJybC5vcmcwggIiMA0GCSqGSIb3DQEBAQUAA4ICDwAwggIKAoIC
AQDu5tx6+GtZiaMAZCGTeNgZ62kZ4Hvs3QYUMJw4UPo9buQ+Ga/OAQ0/DWMJl6Lc
zcfGm0OFzQAJTGezCaW7GRsmqgE9yrDUCcF0fgOuIrV6GnKppcHyDGOSjaORPv1C
B2Qwv3TmAHhe3lqxgA7hnWE+CivVumL7/kVEnlGQpWgkgVQ831qiozRzzkgNb9jY
KF2qmleGjC7iu+xneNc9iy3p0I/5+SAkxzuScR/Egy8UR3zz0T3blDIySvmv2cUM
0WXGUQAYEGULr59cQd1/q0Ommtiz2S4XYWEBWaD6jK4JTy5fbloIClJWnZ10/kQu
y167PcUBVkD8I6BVGPXK66EO2MvpgScM3e3qNYQgCch2rVaolHzmCjdhB1H2bVKU
aYevuGiXyuV8XG3yotSOfRVrUS+8RnJ1yHpmzxVl/ZOvG/CjhpQNptVVqggUw5ht
/4wk8ms9pw6RywOc8vVsOMIgaO3lyiZHBrtYCYfDEfWv+yuNQnaPu1fwWLKqHZoz
wqN1P7PnDSszbvRTYWglDQjK4He7+xVP9hWTupuYGK5/wMZiP+EYb9DBSVVuXPUF
iS3RyG9Ch9c/aqKUUYJ0UDofqlFxnzTdl9L1T1IuqDD1MIPsLRhZvQgzLjCseVOf
/K95s2d7DEjNRdVLL5pq1A7tdSWo2eqwgUebbv0/GR6PewIDAQABo4IBPDCCATgw
HQYDVR0OBBYEFEt2H+7kQF6SuCaguR8mJxeLt2EgMIIBBwYDVR0jBIH/MIH8gBRL
dh/u5EBekrgmoLkfJicXi7dhIKGB2KSB1TCB0jELMAkGA1UEBhMCVVMxCzAJBgNV
BAgTAkNUMRIwEAYDVQQHEwlOZXdpbmd0b24xJDAiBgNVBAoTG0FtZXJpY2FuIFJh
ZGlvIFJlbGF5IExlYWd1ZTEdMBsGA1UECxMUTG9nYm9vayBvZiB0aGUgV29ybGQx
JTAjBgNVBAMTHExvZ2Jvb2sgb2YgdGhlIFdvcmxkIFJvb3QgQ0ExGDAWBgoJkiaJ
k/IsZAEZFghhcnJsLm9yZzEcMBoGCSqGSIb3DQEJARYNbG90d0BhcnJsLm9yZ4IJ
ALbcCREWTZxRMAwGA1UdEwQFMAMBAf8wDQYJKoZIhvcNAQENBQADggIBAJi97ToZ
WGHJ0T6wsTyXa41O7FRzKV7kiTb56lpmWf86eKPxfOLHjkeIJz1DDwyUXOrCrJIX
t8lKdQCmAZf+j3AD7VMb7JfCkFpdT7lP5j0gOfuqesh7GFXYVsCzpQy8vb79cQ5Q
/aGErPyPb4e52cZMHpBGuJy7Y+4a/b5UV6NRkN+Jo8yE7jaYimzNBElHtn8dseW0
/kU6Gvwk+3YwJUzhrwXApMC4Ne0tEWbWO/2eXKex6GoU+n0Xc046peslXFPm2e5v
qBba1PH3Rk+DhpjLQBqZKYYB5oZazEHELQHOlmso5kjQCeQB/5Mc2nxuVKNhsY2W
vm53A43dHBOoNPQa9MbDw5bnSGD4byuP0/EfPUb0PYV9jNhXyzWJywO+Vprozj7C
FcRZNYKpxbVNopyU1YBqDj4LXZUCUR4jmk62EUMyvWKDBa5wpbhQZ9Ef+Xo4Uuy2
M1Q8rXri1LUhTnic1hNyfMw1T2WFwhWw6kaN+uvEuU0bYdbO/SoVqYeMgm6x0IyC
a0vuuj+Tj/X0v3CJt8QLRneqb88tIFy83kQLlK1WbRNFd8HJHP6msJVWlcEBrm+T
kjButHXG2m0MB0xLpRVfWtXLQCxSxeCK/nD1LOkgCkBO0PV0Vjw7U262KmwirveO
XpEKeQDBu2XkTgK8GkeeyYeCEgrS9hGUFeRw
-----END CERTIFICATE-----`

// VerifyARRLChain verifies that cert chains up to the pinned ARRL LoTW Root CA
// via the pinned ARRL LoTW Production CA (or any intermediates supplied in caChain).
//
// Returns nil on success, a descriptive error otherwise.
// Callers should pass the []*x509.Certificate from pkcs12.DecodeChain as caChain.
func VerifyARRLChain(cert *x509.Certificate, caChain []*x509.Certificate) error {
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM([]byte(arrlRootCAPEM)) {
		return fmt.Errorf("failed to parse pinned ARRL Root CA — binary may be corrupt")
	}

	intermediates := x509.NewCertPool()
	if !intermediates.AppendCertsFromPEM([]byte(arrlProductionCAPEM)) {
		return fmt.Errorf("failed to parse pinned ARRL Production CA — binary may be corrupt")
	}
	// Also include any additional intermediates from the .p12 CA chain.
	for _, ca := range caChain {
		intermediates.AddCert(ca)
	}

	opts := x509.VerifyOptions{
		Roots:         roots,
		Intermediates: intermediates,
		CurrentTime:   time.Now(),
		// ARRL LoTW certs are used for digital signatures only.
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	}

	if _, err := cert.Verify(opts); err != nil {
		return fmt.Errorf("certificate does not chain to ARRL LoTW trust store: %w", err)
	}
	return nil
}
