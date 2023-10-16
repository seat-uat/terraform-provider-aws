// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package eks

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/YakDriver/regexache"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	sdkid "github.com/hashicorp/terraform-plugin-sdk/v2/helper/id"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
	"github.com/hashicorp/terraform-provider-aws/internal/conns"
	"github.com/hashicorp/terraform-provider-aws/internal/enum"
	"github.com/hashicorp/terraform-provider-aws/internal/errs"
	"github.com/hashicorp/terraform-provider-aws/internal/errs/sdkdiag"
	tftags "github.com/hashicorp/terraform-provider-aws/internal/tags"
	"github.com/hashicorp/terraform-provider-aws/internal/tfresource"
	"github.com/hashicorp/terraform-provider-aws/internal/verify"
	"github.com/hashicorp/terraform-provider-aws/names"
)

// @SDKResource("aws_eks_addon", name="Add-On")
// @Tags(identifierAttribute="arn")
func ResourceAddon() *schema.Resource {
	return &schema.Resource{
		CreateWithoutTimeout: resourceAddonCreate,
		ReadWithoutTimeout:   resourceAddonRead,
		UpdateWithoutTimeout: resourceAddonUpdate,
		DeleteWithoutTimeout: resourceAddonDelete,

		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		CustomizeDiff: verify.SetTagsDiff,

		Timeouts: &schema.ResourceTimeout{
			Create: schema.DefaultTimeout(20 * time.Minute),
			Update: schema.DefaultTimeout(20 * time.Minute),
			Delete: schema.DefaultTimeout(40 * time.Minute),
		},

		Schema: map[string]*schema.Schema{
			"addon_name": {
				Type:         schema.TypeString,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: validation.NoZeroValues,
			},
			"addon_version": {
				Type:     schema.TypeString,
				Optional: true,
				Computed: true,
				ValidateFunc: validation.All(
					// Regular expression taken from: https://semver.org/#is-there-a-suggested-regular-expression-regex-to-check-a-semver-string
					validation.StringMatch(regexache.MustCompile(`^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-((?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*))*))?(?:\+([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$`), "must follow semantic version format"),
				),
			},
			"arn": {
				Type:     schema.TypeString,
				Computed: true,
			},
			"cluster_name": {
				Type:         schema.TypeString,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: validClusterName,
			},
			"configuration_values": {
				Type:     schema.TypeString,
				Optional: true,
				Computed: true,
			},
			"created_at": {
				Type:     schema.TypeString,
				Computed: true,
			},
			"modified_at": {
				Type:     schema.TypeString,
				Computed: true,
			},
			"preserve": {
				Type:     schema.TypeBool,
				Optional: true,
			},
			"resolve_conflicts": {
				Type:             schema.TypeString,
				Optional:         true,
				ValidateDiagFunc: enum.Validate[types.ResolveConflicts](),
				Deprecated:       `The "resolve_conflicts" attribute can't be set to "PRESERVE" on initial resource creation. Use "resolve_conflicts_on_create" and/or "resolve_conflicts_on_update" instead`,
			},
			"resolve_conflicts_on_create": {
				Type:          schema.TypeString,
				Optional:      true,
				ValidateFunc:  validation.StringInSlice(enum.Slice(types.ResolveConflictsNone, types.ResolveConflictsOverwrite), false),
				ConflictsWith: []string{"resolve_conflicts"},
			},
			"resolve_conflicts_on_update": {
				Type:             schema.TypeString,
				Optional:         true,
				ValidateDiagFunc: enum.Validate[types.ResolveConflicts](),
				ConflictsWith:    []string{"resolve_conflicts"},
			},
			"service_account_role_arn": {
				Type:         schema.TypeString,
				Optional:     true,
				ValidateFunc: verify.ValidARN,
			},
			names.AttrTags:    tftags.TagsSchema(),
			names.AttrTagsAll: tftags.TagsSchemaComputed(),
		},
	}
}

func resourceAddonCreate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	var diags diag.Diagnostics
	client := meta.(*conns.AWSClient).EKSClient(ctx)

	addonName := d.Get("addon_name").(string)
	clusterName := d.Get("cluster_name").(string)
	id := AddonCreateResourceID(clusterName, addonName)
	input := &eks.CreateAddonInput{
		AddonName:          aws.String(addonName),
		ClientRequestToken: aws.String(sdkid.UniqueId()),
		ClusterName:        aws.String(clusterName),
		Tags:               getTagsIn(ctx),
	}

	if v, ok := d.GetOk("addon_version"); ok {
		input.AddonVersion = aws.String(v.(string))
	}

	if v, ok := d.GetOk("configuration_values"); ok {
		input.ConfigurationValues = aws.String(v.(string))
	}

	if v, ok := d.GetOk("resolve_conflicts"); ok {
		input.ResolveConflicts = types.ResolveConflicts(v.(string))
	} else if v, ok := d.GetOk("resolve_conflicts_on_create"); ok {
		input.ResolveConflicts = types.ResolveConflicts(v.(string))
	}

	if v, ok := d.GetOk("service_account_role_arn"); ok {
		input.ServiceAccountRoleArn = aws.String(v.(string))
	}

	_, err := tfresource.RetryWhen(ctx, propagationTimeout,
		func() (interface{}, error) {
			return client.CreateAddon(ctx, input)
		},
		func(err error) (bool, error) {
			if errs.IsA[*types.InvalidParameterException](err) {
				if strings.Contains(err.Error(), "CREATE_FAILED") || strings.Contains(err.Error(), "does not exist") {
					return true, err
				}
			}

			return false, err
		},
	)

	if err != nil {
		return sdkdiag.AppendErrorf(diags, "creating EKS Add-On (%s): %s", id, err)
	}

	d.SetId(id)

	waiter := eks.NewAddonActiveWaiter(client)
	waiterParams := &eks.DescribeAddonInput{
		AddonName:   aws.String(addonName),
		ClusterName: aws.String(clusterName),
	}

	err = waiter.Wait(ctx, waiterParams, d.Timeout(schema.TimeoutCreate))
	if err != nil {
		// Creating addon w/o setting resolve_conflicts to "OVERWRITE"
		// might result in a failed creation, if unmanaged version of addon is already deployed
		// and there are configuration conflicts:
		// ConfigurationConflict	Apply failed with 1 conflict: conflict with "kubectl"...
		//
		// Addon resource is tainted after failed creation, thus will be deleted and created again.
		// Re-creating like this will resolve the error, but it will also purge any
		// configurations that were applied by the user (that were conflicting). This might we an unwanted
		// side effect and should be left for the user to decide how to handle it.
		diags = sdkdiag.AppendErrorf(diags, "waiting for EKS Add-On (%s) create: %s", d.Id(), err)
		return sdkdiag.AppendWarningf(diags, "Running terraform apply again will remove the kubernetes add-on and attempt to create it again effectively purging previous add-on configuration")
	}

	return append(diags, resourceAddonRead(ctx, d, meta)...)
}

func resourceAddonRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	var diags diag.Diagnostics
	client := meta.(*conns.AWSClient).EKSClient(ctx)

	clusterName, addonName, err := AddonParseResourceID(d.Id())

	if err != nil {
		return sdkdiag.AppendFromErr(diags, err)
	}

	addon, err := FindAddonByClusterNameAndAddonName(ctx, client, clusterName, addonName)

	if !d.IsNewResource() && errs.IsA[*types.ResourceNotFoundException](err) {
		if !d.IsNewResource() {
			log.Printf("[WARN] EKS Add-On (%s) not found, removing from state", d.Id())
			d.SetId("")
			return nil
		}
	}

	if err != nil {
		return sdkdiag.AppendErrorf(diags, "reading EKS Add-On (%s): %s", d.Id(), err)
	}

	d.Set("addon_name", addon.AddonName)
	d.Set("addon_version", addon.AddonVersion)
	d.Set("arn", addon.AddonArn)
	d.Set("cluster_name", addon.ClusterName)
	d.Set("configuration_values", addon.ConfigurationValues)
	d.Set("created_at", aws.ToTime(addon.CreatedAt).Format(time.RFC3339))
	d.Set("modified_at", aws.ToTime(addon.ModifiedAt).Format(time.RFC3339))
	d.Set("service_account_role_arn", addon.ServiceAccountRoleArn)

	setTagsOut(ctx, addon.Tags)

	return diags
}

func resourceAddonUpdate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	var diags diag.Diagnostics
	client := meta.(*conns.AWSClient).EKSClient(ctx)

	clusterName, addonName, err := AddonParseResourceID(d.Id())

	if err != nil {
		return sdkdiag.AppendFromErr(diags, err)
	}

	if d.HasChanges("addon_version", "service_account_role_arn", "configuration_values") {
		input := &eks.UpdateAddonInput{
			AddonName:          aws.String(addonName),
			ClientRequestToken: aws.String(sdkid.UniqueId()),
			ClusterName:        aws.String(clusterName),
		}

		if d.HasChange("addon_version") {
			input.AddonVersion = aws.String(d.Get("addon_version").(string))
		}

		if d.HasChange("configuration_values") {
			input.ConfigurationValues = aws.String(d.Get("configuration_values").(string))
		}

		var conflictResolutionAttr string
		var conflictResolution types.ResolveConflicts

		if v, ok := d.GetOk("resolve_conflicts"); ok {
			conflictResolutionAttr = "resolve_conflicts"
			conflictResolution = types.ResolveConflicts(v.(string))
			input.ResolveConflicts = conflictResolution
		} else if v, ok := d.GetOk("resolve_conflicts_on_update"); ok {
			conflictResolutionAttr = "resolve_conflicts_on_update"
			conflictResolution = types.ResolveConflicts(v.(string))
			input.ResolveConflicts = conflictResolution
		}

		// If service account role ARN is already provided, use it. Otherwise, the add-on uses
		// permissions assigned to the node IAM role.
		if d.HasChange("service_account_role_arn") || d.Get("service_account_role_arn").(string) != "" {
			input.ServiceAccountRoleArn = aws.String(d.Get("service_account_role_arn").(string))
		}

		output, err := client.UpdateAddon(ctx, input)

		if err != nil {
			return sdkdiag.AppendErrorf(diags, "updating EKS Add-On (%s): %s", d.Id(), err)
		}

		updateID := aws.ToString(output.Update.Id)
		if _, err := waitAddonUpdateSuccessful(ctx, client, clusterName, addonName, updateID, d.Timeout(schema.TimeoutUpdate)); err != nil {
			if conflictResolution != types.ResolveConflictsOverwrite {
				// Changing addon version w/o setting resolve_conflicts to "OVERWRITE"
				// might result in a failed update if there are conflicts:
				// ConfigurationConflict	Apply failed with 1 conflict: conflict with "kubectl"...
				return sdkdiag.AppendErrorf(diags, "waiting for EKS Add-On (%s) update (%s): %s. Consider setting attribute %q to %q", d.Id(), updateID, err, conflictResolutionAttr, string(types.ResolveConflictsOverwrite))
			}

			return sdkdiag.AppendErrorf(diags, "waiting for EKS Add-On (%s) update (%s): %s", d.Id(), updateID, err)
		}
	}

	return append(diags, resourceAddonRead(ctx, d, meta)...)
}

func resourceAddonDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	var diags diag.Diagnostics
	client := meta.(*conns.AWSClient).EKSClient(ctx)

	clusterName, addonName, err := AddonParseResourceID(d.Id())

	if err != nil {
		return sdkdiag.AppendFromErr(diags, err)
	}

	input := &eks.DeleteAddonInput{
		AddonName:   aws.String(addonName),
		ClusterName: aws.String(clusterName),
	}
	if v, ok := d.GetOk("preserve"); ok {
		input.Preserve = v.(bool)
	}

	log.Printf("[DEBUG] Deleting EKS Add-On: %s", d.Id())
	_, err = client.DeleteAddon(ctx, input)

	if err != nil {
		return sdkdiag.AppendErrorf(diags, "deleting EKS Add-On (%s): %s", d.Id(), err)
	}

	waiter := eks.NewAddonDeletedWaiter(client)
	waiterParams := &eks.DescribeAddonInput{
		AddonName:   aws.String(addonName),
		ClusterName: aws.String(clusterName),
	}

	err = waiter.Wait(ctx, waiterParams, d.Timeout(schema.TimeoutDelete))
	if err != nil {
		sdkdiag.AppendErrorf(diags, "waiting for EKS Add-On (%s) delete: %s", d.Id(), err)
	}

	return diags
}
