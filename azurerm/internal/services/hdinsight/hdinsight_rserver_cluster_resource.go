package hdinsight

import (
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/Azure/azure-sdk-for-go/services/preview/hdinsight/mgmt/2018-06-01-preview/hdinsight"
	"github.com/hashicorp/terraform-plugin-sdk/helper/schema"
	"github.com/terraform-providers/terraform-provider-azurerm/azurerm/helpers/azure"
	"github.com/terraform-providers/terraform-provider-azurerm/azurerm/helpers/tf"
	"github.com/terraform-providers/terraform-provider-azurerm/azurerm/internal/clients"
	"github.com/terraform-providers/terraform-provider-azurerm/azurerm/internal/features"
	"github.com/terraform-providers/terraform-provider-azurerm/azurerm/internal/tags"
	"github.com/terraform-providers/terraform-provider-azurerm/azurerm/internal/timeouts"
	"github.com/terraform-providers/terraform-provider-azurerm/azurerm/utils"
)

// NOTE: this isn't a recommended way of building resources in Terraform
// this pattern is used to work around a generic but pedantic API endpoint
var hdInsightRServerClusterHeadNodeDefinition = azure.HDInsightNodeDefinition{
	CanSpecifyInstanceCount:  false,
	MinInstanceCount:         2,
	MaxInstanceCount:         utils.Int(2),
	CanSpecifyDisks:          false,
	FixedMinInstanceCount:    utils.Int32(int32(1)),
	FixedTargetInstanceCount: utils.Int32(int32(2)),
}

var hdInsightRServerClusterWorkerNodeDefinition = azure.HDInsightNodeDefinition{
	CanSpecifyInstanceCount: true,
	MinInstanceCount:        1,
	CanSpecifyDisks:         false,
}

var hdInsightRServerClusterZookeeperNodeDefinition = azure.HDInsightNodeDefinition{
	CanSpecifyInstanceCount:  false,
	MinInstanceCount:         3,
	MaxInstanceCount:         utils.Int(3),
	CanSpecifyDisks:          false,
	FixedMinInstanceCount:    utils.Int32(int32(1)),
	FixedTargetInstanceCount: utils.Int32(int32(3)),
}

var hdInsightRServerClusterEdgeNodeDefinition = azure.HDInsightNodeDefinition{
	CanSpecifyInstanceCount:  false,
	MinInstanceCount:         1,
	MaxInstanceCount:         utils.Int(1),
	CanSpecifyDisks:          false,
	FixedTargetInstanceCount: utils.Int32(int32(1)),
}

func resourceArmHDInsightRServerCluster() *schema.Resource {
	return &schema.Resource{
		Create: resourceArmHDInsightRServerClusterCreate,
		Read:   resourceArmHDInsightRServerClusterRead,
		Update: hdinsightClusterUpdate("RServer", resourceArmHDInsightRServerClusterRead),
		Delete: hdinsightClusterDelete("RServer"),
		Importer: &schema.ResourceImporter{
			State: schema.ImportStatePassthrough,
		},

		Timeouts: &schema.ResourceTimeout{
			Create: schema.DefaultTimeout(60 * time.Minute),
			Read:   schema.DefaultTimeout(5 * time.Minute),
			Update: schema.DefaultTimeout(60 * time.Minute),
			Delete: schema.DefaultTimeout(60 * time.Minute),
		},

		Schema: map[string]*schema.Schema{
			"name": azure.SchemaHDInsightName(),

			"resource_group_name": azure.SchemaResourceGroupName(),

			"location": azure.SchemaLocation(),

			"cluster_version": azure.SchemaHDInsightClusterVersion(),

			"tier": azure.SchemaHDInsightTier(),

			"tls_min_version": azure.SchemaHDInsightTls(),

			"gateway": azure.SchemaHDInsightsGateway(),

			"rstudio": {
				Type:     schema.TypeBool,
				Required: true,
				ForceNew: true,
			},

			"metastores": azure.SchemaHDInsightsExternalMetastores(),

			"storage_account": azure.SchemaHDInsightsStorageAccounts(),

			"roles": {
				Type:     schema.TypeList,
				Required: true,
				MaxItems: 1,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"head_node": azure.SchemaHDInsightNodeDefinition("roles.0.head_node", hdInsightRServerClusterHeadNodeDefinition),

						"worker_node": azure.SchemaHDInsightNodeDefinition("roles.0.worker_node", hdInsightRServerClusterWorkerNodeDefinition),

						"zookeeper_node": azure.SchemaHDInsightNodeDefinition("roles.0.zookeeper_node", hdInsightRServerClusterZookeeperNodeDefinition),

						"edge_node": azure.SchemaHDInsightNodeDefinition("roles.0.edge_node", hdInsightRServerClusterEdgeNodeDefinition),
					},
				},
			},

			"tags": tags.Schema(),

			"edge_ssh_endpoint": {
				Type:     schema.TypeString,
				Computed: true,
			},

			"https_endpoint": {
				Type:     schema.TypeString,
				Computed: true,
			},

			"ssh_endpoint": {
				Type:     schema.TypeString,
				Computed: true,
			},
		},
	}
}

func expandHDInsightsRServerConfigurations(gateway []interface{}, rStudio bool) map[string]interface{} {
	config := azure.ExpandHDInsightsConfigurations(gateway)

	config["rserver"] = map[string]interface{}{
		"rstudio": rStudio,
	}

	return config
}

func resourceArmHDInsightRServerClusterCreate(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).HDInsight.ClustersClient
	ctx, cancel := timeouts.ForCreate(meta.(*clients.Client).StopContext, d)
	defer cancel()

	name := d.Get("name").(string)
	resourceGroup := d.Get("resource_group_name").(string)
	location := azure.NormalizeLocation(d.Get("location").(string))
	clusterVersion := d.Get("cluster_version").(string)
	t := d.Get("tags").(map[string]interface{})
	tier := hdinsight.Tier(d.Get("tier").(string))
	tls := d.Get("tls_min_version").(string)

	gatewayRaw := d.Get("gateway").([]interface{})
	rStudio := d.Get("rstudio").(bool)
	configurations := expandHDInsightsMLServicesConfigurations(gatewayRaw, rStudio)

	if metastoresRaw, ok := d.GetOkExists("metastores"); ok {
		metastores := expandHDInsightsMetastore(metastoresRaw.([]interface{}))
		for k, v := range metastores {
			configurations[k] = v
		}
	}

	storageAccountsRaw := d.Get("storage_account").([]interface{})
	storageAccounts, identity, err := azure.ExpandHDInsightsStorageAccounts(storageAccountsRaw, nil)
	if err != nil {
		return fmt.Errorf("Error expanding `storage_account`: %s", err)
	}

	RServerRoles := hdInsightRoleDefinition{
		HeadNodeDef:      hdInsightRServerClusterHeadNodeDefinition,
		WorkerNodeDef:    hdInsightRServerClusterWorkerNodeDefinition,
		ZookeeperNodeDef: hdInsightRServerClusterZookeeperNodeDefinition,
		EdgeNodeDef:      &hdInsightRServerClusterEdgeNodeDefinition,
	}
	rolesRaw := d.Get("roles").([]interface{})
	roles, err := expandHDInsightRoles(rolesRaw, RServerRoles)
	if err != nil {
		return fmt.Errorf("Error expanding `roles`: %+v", err)
	}

	if features.ShouldResourcesBeImported() {
		existing, err := client.Get(ctx, resourceGroup, name)
		if err != nil {
			if !utils.ResponseWasNotFound(existing.Response) {
				return fmt.Errorf("Error checking for presence of existing HDInsight RServer Cluster %q (Resource Group %q): %+v", name, resourceGroup, err)
			}
		}

		if existing.ID != nil && *existing.ID != "" {
			return tf.ImportAsExistsError("azurerm_hdinsight_rserver_cluster", *existing.ID)
		}
	}

	params := hdinsight.ClusterCreateParametersExtended{
		Location: utils.String(location),
		Properties: &hdinsight.ClusterCreateProperties{
			Tier:                   tier,
			OsType:                 hdinsight.Linux,
			ClusterVersion:         utils.String(clusterVersion),
			MinSupportedTLSVersion: utils.String(tls),
			ClusterDefinition: &hdinsight.ClusterDefinition{
				Kind:           utils.String("RServer"),
				Configurations: configurations,
			},
			StorageProfile: &hdinsight.StorageProfile{
				Storageaccounts: storageAccounts,
			},
			ComputeProfile: &hdinsight.ComputeProfile{
				Roles: roles,
			},
		},
		Tags:     tags.Expand(t),
		Identity: identity,
	}
	future, err := client.Create(ctx, resourceGroup, name, params)
	if err != nil {
		return fmt.Errorf("Error creating HDInsight RServer Cluster %q (Resource Group %q): %+v", name, resourceGroup, err)
	}

	if err := future.WaitForCompletionRef(ctx, client.Client); err != nil {
		return fmt.Errorf("Error waiting for creation of HDInsight RServer Cluster %q (Resource Group %q): %+v", name, resourceGroup, err)
	}

	read, err := client.Get(ctx, resourceGroup, name)
	if err != nil {
		return fmt.Errorf("Error retrieving HDInsight RServer Cluster %q (Resource Group %q): %+v", name, resourceGroup, err)
	}

	if read.ID == nil {
		return fmt.Errorf("Error reading ID for HDInsight RServer Cluster %q (Resource Group %q)", name, resourceGroup)
	}

	d.SetId(*read.ID)

	return resourceArmHDInsightRServerClusterRead(d, meta)
}

func resourceArmHDInsightRServerClusterRead(d *schema.ResourceData, meta interface{}) error {
	clustersClient := meta.(*clients.Client).HDInsight.ClustersClient
	configurationsClient := meta.(*clients.Client).HDInsight.ConfigurationsClient
	ctx, cancel := timeouts.ForRead(meta.(*clients.Client).StopContext, d)
	defer cancel()

	id, err := azure.ParseAzureResourceID(d.Id())
	if err != nil {
		return err
	}

	resourceGroup := id.ResourceGroup
	name := id.Path["clusters"]

	resp, err := clustersClient.Get(ctx, resourceGroup, name)
	if err != nil {
		if utils.ResponseWasNotFound(resp.Response) {
			log.Printf("[DEBUG] HDInsight RServer Cluster %q was not found in Resource Group %q - removing from state!", name, resourceGroup)
			d.SetId("")
			return nil
		}

		return fmt.Errorf("Error retrieving HDInsight RServer Cluster %q (Resource Group %q): %+v", name, resourceGroup, err)
	}

	// Each call to configurationsClient methods is HTTP request. Getting all settings in one operation
	configurations, err := configurationsClient.List(ctx, resourceGroup, name)
	if err != nil {
		return fmt.Errorf("Error retrieving Configuration for HDInsight MLServices Cluster %q (Resource Group %q): %+v", name, resourceGroup, err)
	}

	gateway, exists := configurations.Configurations["gateway"]
	if !exists {
		return fmt.Errorf("Error retrieving gateway for HDInsight MLServices Cluster %q (Resource Group %q): %+v", name, resourceGroup, err)
	}

	rStudioConfig, exists := configurations.Configurations["rserver"]
	if !exists {
		return fmt.Errorf("Error retrieving RStudio Configuration for HDInsight MLServices Cluster %q (Resource Group %q): %+v", name, resourceGroup, err)
	}

	d.Set("name", name)
	d.Set("resource_group_name", resourceGroup)
	if location := resp.Location; location != nil {
		d.Set("location", azure.NormalizeLocation(*location))
	}

	// storage_account isn't returned so I guess we just leave it ¯\_(ツ)_/¯
	if props := resp.Properties; props != nil {
		d.Set("cluster_version", props.ClusterVersion)
		d.Set("tier", string(props.Tier))
		d.Set("tls_min_version", props.MinSupportedTLSVersion)

		if def := props.ClusterDefinition; def != nil {
			if err := d.Set("gateway", azure.FlattenHDInsightsConfigurations(gateway)); err != nil {
				return fmt.Errorf("Error flattening `gateway`: %+v", err)
			}

			flattenHDInsightsMetastores(d, configurations.Configurations)

			var rStudio bool
			if rStudioStr := rStudioConfig["rstudio"]; rStudioStr != nil {
				rStudioBool, err := strconv.ParseBool(*rStudioStr)
				if err != nil {
					return err
				}

				rStudio = rStudioBool
			}

			d.Set("rstudio", rStudio)
		}

		RServerRoles := hdInsightRoleDefinition{
			HeadNodeDef:      hdInsightRServerClusterHeadNodeDefinition,
			WorkerNodeDef:    hdInsightRServerClusterWorkerNodeDefinition,
			ZookeeperNodeDef: hdInsightRServerClusterZookeeperNodeDefinition,
			EdgeNodeDef:      &hdInsightRServerClusterEdgeNodeDefinition,
		}
		flattenedRoles := flattenHDInsightRoles(d, props.ComputeProfile, RServerRoles)
		if err := d.Set("roles", flattenedRoles); err != nil {
			return fmt.Errorf("Error flattening `roles`: %+v", err)
		}

		edgeSSHEndpoint := azure.FindHDInsightConnectivityEndpoint("EDGESSH", props.ConnectivityEndpoints)
		d.Set("edge_ssh_endpoint", edgeSSHEndpoint)
		httpEndpoint := azure.FindHDInsightConnectivityEndpoint("HTTPS", props.ConnectivityEndpoints)
		d.Set("https_endpoint", httpEndpoint)
		sshEndpoint := azure.FindHDInsightConnectivityEndpoint("SSH", props.ConnectivityEndpoints)
		d.Set("ssh_endpoint", sshEndpoint)
	}

	return tags.FlattenAndSet(d, resp.Tags)
}
