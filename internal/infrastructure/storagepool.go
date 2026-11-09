package infrastructure

import (
	"fmt"
	"strings"

	"github.com/pulumi/pulumi-terraform-provider/sdks/go/libvirt/libvirt"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/y0n1/virtlab/internal/config"
)

// BuildStoragePool creates the isolated directory storage pool for the cluster.
// Invariant: Pool MUST be at /home/libvirt/<clusterName>/pool. The 'default' pool is strictly forbidden.
func BuildStoragePool(ctx *pulumi.Context, cfg *config.Config) (*libvirt.Pool, error) {
	poolName := cfg.ClusterName
	if strings.ToLower(poolName) == "default" {
		return nil, fmt.Errorf("infrastructure: 'default' storage pool is strictly forbidden; clusterName must be distinct")
	}

	poolDir := cfg.StoragePoolDir()
	expectedPrefix := fmt.Sprintf("/home/libvirt/%s/pool", cfg.ClusterName)
	if poolDir != expectedPrefix {
		return nil, fmt.Errorf("infrastructure isolation violation: poolDir '%s' does not match '%s'", poolDir, expectedPrefix)
	}

	pool, err := libvirt.NewPool(ctx, "lab-pool", &libvirt.PoolArgs{
		Name:   pulumi.String(poolName),
		Type:   pulumi.String("dir"),
		Target: &libvirt.PoolTargetArgs{Path: pulumi.String(poolDir)},
		Create: &libvirt.PoolCreateArgs{
			Autostart: pulumi.Bool(true),
			Build:     pulumi.Bool(true),
			Start:     pulumi.Bool(true),
		},
		Destroy: &libvirt.PoolDestroyArgs{
			Delete: pulumi.Bool(false),
		},
	}, pulumi.ReplaceOnChanges([]string{"*"}), pulumi.DeleteBeforeReplace(true))
	if err != nil {
		return nil, fmt.Errorf("infrastructure: create pool '%s': %w", poolName, err)
	}

	return pool, nil
}
