---
subcategory: "Mongo Cluster"
layout: "azurerm"
page_title: "Azure Resource Manager: azurerm_mongo_cluster_promote"
description: |-
  Promotes a replica Azure Cosmos DB for MongoDB vCore cluster to primary.
---

# Action: azurerm_mongo_cluster_promote

Promotes an Azure Cosmos DB for MongoDB vCore replica cluster to primary using `Switchover` mode and waits for the operation to complete.

`Planned` waits for the replica to catch up before promotion, preventing data loss during the promotion. `Forced` does not wait for the replica to catch up and is intended for disaster recovery.

~> **Note:** Terraform 1.14 or later is required to use actions. Planned promotion uses the `2026-06-15-preview` Azure API. Forced promotion uses the stable `2026-06-01` API.

!> **Note:** Forced promotion can result in data loss. Promotion changes cluster roles and can interrupt application connections. Verify that the target is a replica and that applications are prepared for the role change before invoking this action.

## Example Usage

Declare the action with the resource ID of the replica, not the current primary:

```hcl
variable "replica_mongo_cluster_id" {
  type        = string
  description = "The resource ID of the replica Mongo Cluster to promote."
}

action "azurerm_mongo_cluster_promote" "example" {
  config {
    mongo_cluster_id = var.replica_mongo_cluster_id
    promote_option   = "Planned"
  }
}
```

Invoke it explicitly:

```shell
terraform apply -invoke=action.azurerm_mongo_cluster_promote.example
```

The action can also be referenced by a resource's `lifecycle.action_trigger` block. Declaring the action alone does not invoke it. Promotion is an imperative operation, not a persistent desired state on `azurerm_mongo_cluster`, and Terraform does not automatically undo it.

~> **Note:** When Terraform manages the participating clusters, review a refreshed plan after promotion. Their replication source can change. The `source_server_id` and `source_location` arguments on `azurerm_mongo_cluster` describe replica creation, and changing them can force replacement. Use `lifecycle.ignore_changes` for those arguments on participating clusters when managing role changes through this action, and review any proposed replacement before applying.

## Argument Reference

This action supports the following arguments:

- `mongo_cluster_id` - (Required) The ID of the replica Mongo Cluster to promote to primary.

- `promote_option` - (Required) The promotion option. Possible values are `Planned` and `Forced`. There is no default; the option must be chosen explicitly.

- `timeout` - (Optional) A positive duration to wait for promotion to complete, such as `60m` or `2h`. Defaults to `60m`.

~> **Note:** A timeout stops Terraform from waiting; it does not cancel an operation already accepted by Azure. Check the cluster roles and operation status before retrying.
