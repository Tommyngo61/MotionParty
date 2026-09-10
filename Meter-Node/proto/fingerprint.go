package meternodeproto

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// HardwareFingerprint is the raw material that identifies a physical node.
//
// It is collected by the agent and sent at enrollment. The controller stores
// the components as well as the hash so that when a fingerprint changes a
// human can see *what* changed — a swapped GPU is a different conversation
// from a swapped motherboard, and both are different from a cloned image
// showing up at a second address.
type HardwareFingerprint struct {
	MotherboardUUID string   `msgpack:"motherboard_uuid" json:"motherboard_uuid"`
	GPUUUIDs        []string `msgpack:"gpu_uuids" json:"gpu_uuids"`
	PrimaryNICMAC   string   `msgpack:"primary_nic_mac" json:"primary_nic_mac"`
}

// FingerprintVersion prefixes the hash input so the algorithm can change
// without a rehashed fleet silently colliding with the old scheme.
const FingerprintVersion = "mnfp1"

// Hash returns the stable hex digest that identifies this hardware.
//
// Normalisation is the whole job here. GPU UUIDs come back from NVML in a
// driver-dependent order, MAC and UUID casing varies between dmidecode and
// sysfs, and any of that jitter would look like a hardware swap and trigger a
// re-attestation on a node that never changed. So: lowercase, trim, sort the
// GPU list, and length-delimit each component so no rearrangement of values
// can produce the same input.
func (f HardwareFingerprint) Hash() string {
	norm := func(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

	gpus := make([]string, 0, len(f.GPUUUIDs))
	for _, g := range f.GPUUUIDs {
		if g = norm(g); g != "" {
			gpus = append(gpus, g)
		}
	}
	sort.Strings(gpus)

	var b strings.Builder
	b.WriteString(FingerprintVersion)
	b.WriteByte('\n')
	write := func(s string) { fmt.Fprintf(&b, "%d:%s\n", len(s), s) }
	write(norm(f.MotherboardUUID))
	write(norm(f.PrimaryNICMAC))
	write(fmt.Sprintf("%d", len(gpus)))
	for _, g := range gpus {
		write(g)
	}

	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// Complete reports whether enough of the fingerprint was collected to be worth
// binding an identity to.
//
// A node that cannot read its motherboard UUID (some consumer boards report
// all-zeros or a vendor placeholder) still enrolls, but on the strength of its
// GPU UUIDs and NIC alone — and the controller records that so the weaker
// binding is visible rather than assumed.
func (f HardwareFingerprint) Complete() bool {
	return f.usableMotherboard() && f.PrimaryNICMAC != "" && len(f.GPUUUIDs) > 0
}

// placeholderUUIDs are values consumer boards return instead of a real DMI
// system UUID. Treating them as identity would make every affected node share
// a fingerprint.
var placeholderUUIDs = map[string]bool{
	"00000000-0000-0000-0000-000000000000": true,
	"ffffffff-ffff-ffff-ffff-ffffffffffff": true,
	"03000200-0400-0500-0006-000700080009": true, // a widely-shipped Gigabyte/MSI default
	"default string":                       true,
	"to be filled by o.e.m.":               true,
	"system serial number":                 true,
}

func (f HardwareFingerprint) usableMotherboard() bool {
	v := strings.ToLower(strings.TrimSpace(f.MotherboardUUID))
	return v != "" && !placeholderUUIDs[v]
}

// Diff reports which components changed between two fingerprints, for the
// re-attestation record and for the operator UI.
func (f HardwareFingerprint) Diff(other HardwareFingerprint) []string {
	var changed []string
	norm := func(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
	if norm(f.MotherboardUUID) != norm(other.MotherboardUUID) {
		changed = append(changed, "motherboard_uuid")
	}
	if norm(f.PrimaryNICMAC) != norm(other.PrimaryNICMAC) {
		changed = append(changed, "primary_nic_mac")
	}
	a := append([]string{}, f.GPUUUIDs...)
	b := append([]string{}, other.GPUUUIDs...)
	for i := range a {
		a[i] = norm(a[i])
	}
	for i := range b {
		b[i] = norm(b[i])
	}
	sort.Strings(a)
	sort.Strings(b)
	if strings.Join(a, ",") != strings.Join(b, ",") {
		changed = append(changed, "gpu_uuids")
	}
	return changed
}
