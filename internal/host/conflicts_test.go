package host

import (
	"testing"
)

func TestParseConflictingLabs(t *testing.T) {
	output := `
default
target-lab-net
other-lab-net
---POOLS---
default
images
target-lab
other-lab
---DIRS---
target-lab
images
other-lab
lost+found
.cache
third-lab
`
	conflicts := parseConflictingLabs(output, "target-lab")

	if len(conflicts) != 2 {
		t.Fatalf("expected 2 conflicts, got %d: %v", len(conflicts), conflicts)
	}

	if conflicts[0] != "other-lab" || conflicts[1] != "third-lab" {
		t.Errorf("unexpected conflicts list: %v", conflicts)
	}
}

func TestParseConflictingLabs_Clean(t *testing.T) {
	output := `
default
my-cluster-net
---POOLS---
default
images
my-cluster
---DIRS---
my-cluster
images
lost+found
.cache
`
	conflicts := parseConflictingLabs(output, "my-cluster")

	if len(conflicts) != 0 {
		t.Fatalf("expected 0 conflicts, got %d: %v", len(conflicts), conflicts)
	}
}

func TestParseConflictingLabs_IgnoresImagesAndSystemEntries(t *testing.T) {
	output := `
default
---POOLS---
default
images
---DIRS---
images
lost+found
.cache
.ssh
`
	conflicts := parseConflictingLabs(output, "fresh-cluster")
	if len(conflicts) != 0 {
		t.Fatalf("expected 0 conflicts on fresh host with images/system resources, got %d: %v", len(conflicts), conflicts)
	}
}

func TestParseConflictingLabs_NormalizesNetworkSuffix(t *testing.T) {
	output := `
my-cluster-net
other-cluster-net
---POOLS---
---DIRS---
`
	conflicts := parseConflictingLabs(output, "my-cluster")
	if len(conflicts) != 1 || conflicts[0] != "other-cluster" {
		t.Fatalf("expected ['other-cluster'], got %v", conflicts)
	}
}

func TestParseExistingClusterOutput(t *testing.T) {
	outExists := `
EXISTS:true
ALLOCATED_RAM:16384
ALLOCATED_DISK:120
`
	res := parseExistingClusterOutput(outExists)
	if !res.Exists {
		t.Errorf("expected Exists=true")
	}
	if res.AllocatedRAMMiB != 16384 {
		t.Errorf("expected AllocatedRAMMiB 16384, got %d", res.AllocatedRAMMiB)
	}
	if res.AllocatedDiskGiB != 120 {
		t.Errorf("expected AllocatedDiskGiB 120, got %d", res.AllocatedDiskGiB)
	}

	outNotExists := `
EXISTS:false
`
	resNone := parseExistingClusterOutput(outNotExists)
	if resNone.Exists {
		t.Errorf("expected Exists=false")
	}
	if resNone.AllocatedRAMMiB != 0 || resNone.AllocatedDiskGiB != 0 {
		t.Errorf("expected 0 allocated resources, got %+v", resNone)
	}
}
