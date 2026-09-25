// Copyright IBM Corp. 2014, 2026
// SPDX-License-Identifier: MPL-2.0

package mongocluster_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/hashicorp/go-azure-helpers/lang/pointer"
	"github.com/hashicorp/go-azure-sdk/resource-manager/mongocluster/2026-06-01/mongoclusters"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"
	"github.com/hashicorp/terraform-provider-azurerm/internal/acceptance"
	"github.com/hashicorp/terraform-provider-azurerm/internal/acceptance/testclient"
	"github.com/hashicorp/terraform-provider-azurerm/internal/provider/framework"
)

type MongoClusterPromoteAction struct{}

func TestAccMongoClusterPromoteAction_planned(t *testing.T) {
	MongoClusterPromoteAction{}.test(t, "Planned")
}

func TestAccMongoClusterPromoteAction_forced(t *testing.T) {
	MongoClusterPromoteAction{}.test(t, "Forced")
}

func (a MongoClusterPromoteAction) test(t *testing.T, option string) {
	data := acceptance.BuildTestData(t, "azurerm_mongo_cluster_promote", "test")
	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { acceptance.PreCheck(t) },
		ProtoV5ProviderFactories: framework.ProtoV5ProviderFactoriesInit(context.Background(), "azurerm"),
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{
			tfversion.SkipBelow(tfversion.Version1_14_0),
		},
		Steps: []resource.TestStep{
			{
				Config: a.config(data, option),
				Check:  a.checkPrimary(),
			},
			{
				RefreshState: true,
				Check:        a.checkPrimary(),
			},
		},
	})
}

func (a MongoClusterPromoteAction) checkPrimary() resource.TestCheckFunc {
	return func(state *terraform.State) error {
		cluster, ok := state.RootModule().Resources["azurerm_mongo_cluster.replica"]
		if !ok || cluster.Primary == nil {
			return fmt.Errorf("replica Mongo Cluster not found in state")
		}
		id, err := mongoclusters.ParseMongoClusterID(cluster.Primary.ID)
		if err != nil {
			return err
		}
		client, err := testclient.Build()
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		response, err := client.MongoCluster.MongoClustersClient.Get(ctx, *id)
		if err != nil {
			return err
		}
		if response.Model == nil || response.Model.Properties == nil || response.Model.Properties.Replica == nil {
			return fmt.Errorf("missing replication properties for %s", id)
		}
		if role := pointer.From(response.Model.Properties.Replica.Role); role != mongoclusters.ReplicationRolePrimary {
			return fmt.Errorf("expected %s to be Primary, got %q", id, role)
		}
		return nil
	}
}

func (a MongoClusterPromoteAction) config(data acceptance.TestData, option string) string {
	return fmt.Sprintf(`
provider "azurerm" {
  features {}
}

resource "azurerm_resource_group" "test" {
  name     = "acctestRG-%[1]d"
  location = "%[2]s"
}

resource "azurerm_mongo_cluster" "source" {
  name                   = "acctest-mc-%[1]d"
  resource_group_name    = azurerm_resource_group.test.name
  location               = azurerm_resource_group.test.location
  administrator_username = "adminTerraform"
  administrator_password = "testQAZwsx123"
  shard_count            = 1
  compute_tier           = "M30"
  high_availability_mode = "ZoneRedundantPreferred"
  storage_size_in_gb     = 64
  version                = "8.0"

  lifecycle {
    ignore_changes = [source_server_id, source_location]
  }
}

resource "azurerm_mongo_cluster" "replica" {
  name                = "acctest-mc-replica-%[1]d"
  resource_group_name = azurerm_resource_group.test.name
  location            = "%[3]s"
  source_server_id    = azurerm_mongo_cluster.source.id
  source_location     = azurerm_mongo_cluster.source.location
  create_mode         = "GeoReplica"

  lifecycle {
    ignore_changes = [
      administrator_username, high_availability_mode, preview_features,
      shard_count, storage_size_in_gb, compute_tier, version,
      source_server_id, source_location,
    ]
  }
}

resource "terraform_data" "trigger" {
  input = azurerm_mongo_cluster.replica.id

  lifecycle {
    action_trigger {
      events  = [after_create]
      actions = [action.azurerm_mongo_cluster_promote.test]
    }
  }
}

action "azurerm_mongo_cluster_promote" "test" {
  config {
    mongo_cluster_id = azurerm_mongo_cluster.replica.id
    promote_option   = "%[4]s"
    timeout          = "90m"
  }
}
`, data.RandomInteger, data.Locations.Primary, data.Locations.Secondary, option)
}
