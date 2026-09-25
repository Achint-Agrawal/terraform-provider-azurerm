// Copyright IBM Corp. 2014, 2026
// SPDX-License-Identifier: MPL-2.0

package mongocluster

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/go-azure-helpers/framework/typehelpers"
	"github.com/hashicorp/go-azure-helpers/lang/pointer"
	"github.com/hashicorp/go-azure-sdk/resource-manager/mongocluster/2026-06-01/mongoclusters"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/action"
	"github.com/hashicorp/terraform-plugin-framework/action/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-provider-azurerm/internal/sdk"
)

type MongoClusterPromoteAction struct {
	sdk.ActionMetadata
}

var _ sdk.Action = &MongoClusterPromoteAction{}

func newMongoClusterPromoteAction() action.Action {
	return &MongoClusterPromoteAction{}
}

type MongoClusterPromoteActionModel struct {
	MongoClusterId types.String `tfsdk:"mongo_cluster_id"`
	PromoteOption  types.String `tfsdk:"promote_option"`
	Timeout        types.String `tfsdk:"timeout"`
}

func (a *MongoClusterPromoteAction) Metadata(_ context.Context, _ action.MetadataRequest, response *action.MetadataResponse) {
	response.TypeName = "azurerm_mongo_cluster_promote"
}

func (a *MongoClusterPromoteAction) Schema(_ context.Context, _ action.SchemaRequest, response *action.SchemaResponse) {
	response.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"mongo_cluster_id": schema.StringAttribute{
				Required:    true,
				Description: "The ID of the replica Mongo Cluster to promote to primary.",
				Validators: []validator.String{
					typehelpers.WrappedStringValidator{Func: mongoclusters.ValidateMongoClusterID},
				},
			},
			"promote_option": schema.StringAttribute{
				Required:    true,
				Description: "The promotion option. Possible values are `Planned` and `Forced`. Planned waits for replication to catch up. Forced can result in data loss.",
				Validators: []validator.String{
					stringvalidator.OneOf("Planned", "Forced"),
				},
			},
			"timeout": schema.StringAttribute{
				Optional:    true,
				Description: "A positive duration to wait for promotion to complete. Defaults to `60m`.",
				Validators: []validator.String{
					typehelpers.WrappedStringValidator{Func: validateMongoClusterPromoteTimeout},
				},
			},
		},
	}
}

func (a *MongoClusterPromoteAction) Configure(ctx context.Context, request action.ConfigureRequest, response *action.ConfigureResponse) {
	a.Defaults(ctx, request, response)
}

func (a *MongoClusterPromoteAction) Invoke(ctx context.Context, request action.InvokeRequest, response *action.InvokeResponse) {
	var model MongoClusterPromoteActionModel
	response.Diagnostics.Append(request.Config.Get(ctx, &model)...)
	if response.Diagnostics.HasError() {
		return
	}

	timeout := 60 * time.Minute
	if !model.Timeout.IsNull() {
		duration, err := time.ParseDuration(model.Timeout.ValueString())
		if err != nil {
			sdk.SetResponseErrorDiagnostic(response, "parsing `timeout`", err)
			return
		}
		if duration <= 0 {
			sdk.SetResponseErrorDiagnostic(response, "parsing `timeout`", "timeout must be a positive duration")
			return
		}
		timeout = duration
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	id, err := mongoclusters.ParseMongoClusterID(model.MongoClusterId.ValueString())
	if err != nil {
		sdk.SetResponseErrorDiagnostic(response, "parsing `mongo_cluster_id`", err)
		return
	}

	option := model.PromoteOption.ValueString()
	response.SendProgress(action.InvokeProgressEvent{
		Message: fmt.Sprintf("promoting %s using %s switchover", id, option),
	})

	switch option {
	case "Planned":
		err = a.Client.MongoCluster.PlannedPromoteClient.PromoteThenPoll(ctx, *id)
	case "Forced":
		err = a.Client.MongoCluster.MongoClustersClient.PromoteThenPoll(ctx, *id, mongoclusters.PromoteReplicaRequest{
			Mode:          pointer.To(mongoclusters.PromoteModeSwitchover),
			PromoteOption: mongoclusters.PromoteOptionForced,
		})
	default:
		sdk.SetResponseErrorDiagnostic(response, "validating `promote_option`", fmt.Sprintf("unsupported promotion option %q", option))
		return
	}
	if err != nil {
		sdk.SetResponseErrorDiagnostic(response, "promoting Mongo Cluster", fmt.Sprintf("promoting %s: %+v", id, err))
		return
	}

	response.SendProgress(action.InvokeProgressEvent{
		Message: fmt.Sprintf("promotion of %s completed", id),
	})
}

func validateMongoClusterPromoteTimeout(value interface{}, key string) ([]string, []error) {
	duration, err := time.ParseDuration(value.(string))
	if err != nil {
		return nil, []error{fmt.Errorf("parsing %s: %+v", key, err)}
	}
	if duration <= 0 {
		return nil, []error{fmt.Errorf("%s must be a positive duration", key)}
	}
	return nil, nil
}
