// Copyright IBM Corp. 2014, 2026
// SPDX-License-Identifier: MPL-2.0

package azuresdkhacks

import (
	"context"

	"github.com/hashicorp/go-azure-helpers/lang/pointer"
	"github.com/hashicorp/go-azure-sdk/resource-manager/mongocluster/2026-06-01/mongoclusters"
	"github.com/hashicorp/go-azure-sdk/sdk/client/resourcemanager"
	"github.com/hashicorp/go-azure-sdk/sdk/environments"
)

// Planned promotion is only available in 2026-06-15-preview. Until that SDK is
// generated, reuse the identical promote request and LRO implementation with an
// isolated client. Do not change the API version used by resource operations.
// https://github.com/Azure/azure-rest-api-specs/pull/44557
type PlannedPromoteClient struct {
	Client *resourcemanager.Client
}

func NewPlannedPromoteClient(api environments.Api) (*PlannedPromoteClient, error) {
	client, err := resourcemanager.NewClient(api, "mongoclusters", "2026-06-15-preview")
	if err != nil {
		return nil, err
	}

	return &PlannedPromoteClient{Client: client}, nil
}

func (c PlannedPromoteClient) PromoteThenPoll(ctx context.Context, id mongoclusters.MongoClusterId) error {
	client := mongoclusters.MongoClustersClient{Client: c.Client}
	return client.PromoteThenPoll(ctx, id, mongoclusters.PromoteReplicaRequest{
		Mode:          pointer.To(mongoclusters.PromoteModeSwitchover),
		PromoteOption: mongoclusters.PromoteOption("Planned"),
	})
}
