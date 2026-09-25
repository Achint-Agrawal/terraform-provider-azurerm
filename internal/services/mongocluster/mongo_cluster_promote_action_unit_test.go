// Copyright IBM Corp. 2014, 2026
// SPDX-License-Identifier: MPL-2.0

package mongocluster_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/hashicorp/go-azure-sdk/resource-manager/mongocluster/2026-06-01/mongoclusters"
	"github.com/hashicorp/go-azure-sdk/sdk/environments"
	"github.com/hashicorp/terraform-plugin-framework/action"
	"github.com/hashicorp/terraform-plugin-framework/action/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/hashicorp/terraform-provider-azurerm/internal/clients"
	"github.com/hashicorp/terraform-provider-azurerm/internal/sdk"
	"github.com/hashicorp/terraform-provider-azurerm/internal/services/mongocluster"
	"github.com/hashicorp/terraform-provider-azurerm/internal/services/mongocluster/azuresdkhacks"
	mongoclusterclient "github.com/hashicorp/terraform-provider-azurerm/internal/services/mongocluster/client"
)

const promoteTestClusterID = "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/test/providers/Microsoft.DocumentDB/mongoClusters/replica"

func TestMongoClusterPromoteAction_schema(t *testing.T) {
	ctx := context.Background()
	a := &mongocluster.MongoClusterPromoteAction{}
	var response action.SchemaResponse
	a.Schema(ctx, action.SchemaRequest{}, &response)

	for _, name := range []string{"mongo_cluster_id", "promote_option"} {
		if !response.Schema.Attributes[name].IsRequired() {
			t.Fatalf("%s must be required", name)
		}
	}
	if !response.Schema.Attributes["timeout"].IsOptional() {
		t.Fatal("timeout must be optional")
	}

	for _, tc := range []struct {
		name      string
		attribute string
		value     types.String
		wantError bool
	}{
		{"cluster", "mongo_cluster_id", types.StringValue(promoteTestClusterID), false},
		{"invalid cluster", "mongo_cluster_id", types.StringValue("replica"), true},
		{"wrong resource", "mongo_cluster_id", types.StringValue(promoteTestClusterID + "/users/user"), true},
		{"unknown cluster", "mongo_cluster_id", types.StringUnknown(), false},
		{"planned", "promote_option", types.StringValue("Planned"), false},
		{"forced", "promote_option", types.StringValue("Forced"), false},
		{"invalid option", "promote_option", types.StringValue("planned"), true},
		{"unknown option", "promote_option", types.StringUnknown(), false},
		{"timeout", "timeout", types.StringValue("90m"), false},
		{"default timeout", "timeout", types.StringNull(), false},
		{"unknown timeout", "timeout", types.StringUnknown(), false},
		{"invalid timeout", "timeout", types.StringValue("tomorrow"), true},
		{"zero timeout", "timeout", types.StringValue("0s"), true},
		{"negative timeout", "timeout", types.StringValue("-1m"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			attribute := response.Schema.Attributes[tc.attribute].(schema.StringAttribute)
			var result validator.StringResponse
			for _, validation := range attribute.Validators {
				validation.ValidateString(ctx, validator.StringRequest{
					Path:        path.Root(tc.attribute),
					ConfigValue: tc.value,
				}, &result)
			}
			if result.Diagnostics.HasError() != tc.wantError {
				t.Fatalf("unexpected diagnostics: %v", result.Diagnostics)
			}
		})
	}
}

func TestMongoClusterPromoteAction_registration(t *testing.T) {
	factories := mongocluster.Registration{}.Actions()
	if len(factories) != 1 {
		t.Fatalf("expected one Mongo Cluster action, got %d", len(factories))
	}
	var response action.MetadataResponse
	factories[0]().Metadata(context.Background(), action.MetadataRequest{}, &response)
	if response.TypeName != "azurerm_mongo_cluster_promote" {
		t.Fatalf("unexpected action name %q", response.TypeName)
	}
}

func TestMongoClusterPromoteAction_invoke(t *testing.T) {
	for _, tc := range []struct {
		name        string
		option      string
		timeout     interface{}
		id          string
		failure     string
		wantVersion string
		wantError   string
		wantPosts   int32
		wantPolls   int32
	}{
		{name: "planned", option: "Planned", wantVersion: "2026-06-15-preview", wantPosts: 1, wantPolls: 2},
		{name: "forced", option: "Forced", timeout: "5m", wantVersion: "2026-06-01", wantPosts: 1, wantPolls: 2},
		{name: "request failure", option: "Planned", failure: "request", wantVersion: "2026-06-15-preview", wantError: "ClusterNotReadReplica", wantPosts: 1},
		{name: "operation failure", option: "Planned", failure: "poll", wantVersion: "2026-06-15-preview", wantError: "polling after Promote", wantPosts: 1, wantPolls: 2},
		{name: "missing polling header", option: "Planned", failure: "header", wantVersion: "2026-06-15-preview", wantError: "no applicable pollers", wantPosts: 1},
		{name: "polling timeout", option: "Planned", failure: "timeout", timeout: "1s", wantVersion: "2026-06-15-preview", wantError: "context deadline exceeded", wantPosts: 1},
		{name: "invalid option", option: "Invalid", wantError: "unsupported promotion option"},
		{name: "invalid id", option: "Planned", id: "invalid", wantError: "parsing `mongo_cluster_id`"},
		{name: "invalid timeout", option: "Planned", timeout: "invalid", wantError: "parsing `timeout`"},
		{name: "zero timeout", option: "Planned", timeout: "0s", wantError: "positive duration"},
		{name: "negative timeout", option: "Planned", timeout: "-1s", wantError: "positive duration"},
		{name: "expired timeout", option: "Planned", timeout: "1ns", wantError: "performing Promote"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var posts, polls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "0")
				switch {
				case r.Method == http.MethodPost && r.URL.Path == promoteTestClusterID+"/promote":
					posts.Add(1)
					if got := r.URL.Query().Get("api-version"); got != tc.wantVersion {
						t.Errorf("api-version = %q, want %q", got, tc.wantVersion)
					}
					var body map[string]string
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Errorf("decoding request: %v", err)
					}
					if len(body) != 2 || body["mode"] != "Switchover" || body["promoteOption"] != tc.option {
						t.Errorf("unexpected root request body: %#v", body)
					}
					if tc.failure == "request" {
						w.WriteHeader(http.StatusBadRequest)
						fmt.Fprint(w, `{"error":{"code":"ClusterNotReadReplica","message":"The target is not a replica."}}`)
						return
					}
					if tc.failure != "header" {
						w.Header().Set("Location", "http://"+r.Host+"/operations/promote?api-version="+tc.wantVersion)
					}
					if tc.failure == "timeout" {
						w.Header().Set("Retry-After", "60")
					}
					w.WriteHeader(http.StatusAccepted)
				case r.Method == http.MethodGet && r.URL.Path == "/operations/promote":
					count := polls.Add(1)
					if got := r.URL.Query().Get("api-version"); got != tc.wantVersion {
						t.Errorf("poll api-version = %q, want %q", got, tc.wantVersion)
					}
					if count == 1 {
						w.WriteHeader(http.StatusAccepted)
						return
					}
					if tc.failure == "poll" {
						fmt.Fprint(w, `{"status":"Failed","error":{"code":"PromotionFailed","message":"Promotion failed."}}`)
						return
					}
					fmt.Fprint(w, `{"status":"Succeeded"}`)
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
					w.WriteHeader(http.StatusBadRequest)
				}
			}))
			defer server.Close()

			stable, err := mongoclusters.NewMongoClustersClientWithBaseURI(environments.AzurePublic().ResourceManager)
			if err != nil {
				t.Fatal(err)
			}
			planned, err := azuresdkhacks.NewPlannedPromoteClient(environments.AzurePublic().ResourceManager)
			if err != nil {
				t.Fatal(err)
			}
			stable.Client.BaseUri = server.URL
			planned.Client.BaseUri = server.URL
			stable.Client.AuthorizeRequest = nil
			planned.Client.AuthorizeRequest = nil

			a := &mongocluster.MongoClusterPromoteAction{
				ActionMetadata: sdk.ActionMetadata{
					Client: &clients.Client{
						MongoCluster: &mongoclusterclient.Client{
							MongoClustersClient:  stable,
							PlannedPromoteClient: planned,
						},
					},
				},
			}
			var actionSchema action.SchemaResponse
			a.Schema(context.Background(), action.SchemaRequest{}, &actionSchema)
			id := tc.id
			if id == "" {
				id = promoteTestClusterID
			}
			request := action.InvokeRequest{Config: tfsdk.Config{
				Schema: actionSchema.Schema,
				Raw: tftypes.NewValue(tftypes.Object{AttributeTypes: map[string]tftypes.Type{
					"mongo_cluster_id": tftypes.String,
					"promote_option":   tftypes.String,
					"timeout":          tftypes.String,
				}}, map[string]tftypes.Value{
					"mongo_cluster_id": tftypes.NewValue(tftypes.String, id),
					"promote_option":   tftypes.NewValue(tftypes.String, tc.option),
					"timeout":          tftypes.NewValue(tftypes.String, tc.timeout),
				}),
			}}
			var completed bool
			response := action.InvokeResponse{SendProgress: func(event action.InvokeProgressEvent) {
				if strings.HasSuffix(event.Message, "completed") {
					completed = true
				}
			}}
			a.Invoke(context.Background(), request, &response)
			if tc.wantError == "" {
				if response.Diagnostics.HasError() || !completed {
					t.Fatalf("expected completed action, got %v", response.Diagnostics)
				}
			} else {
				if !response.Diagnostics.HasError() || completed {
					t.Fatalf("expected failed action, got %v", response.Diagnostics)
				}
				if !strings.Contains(fmt.Sprint(response.Diagnostics), tc.wantError) {
					t.Fatalf("expected %q, got %v", tc.wantError, response.Diagnostics)
				}
			}
			if posts.Load() != tc.wantPosts || polls.Load() != tc.wantPolls {
				t.Fatalf("requests: POST=%d, poll=%d; want POST=%d, poll=%d", posts.Load(), polls.Load(), tc.wantPosts, tc.wantPolls)
			}
		})
	}
}
