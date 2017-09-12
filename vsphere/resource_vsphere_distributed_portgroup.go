package vsphere

import (
	"fmt"
	"log"

	"github.com/hashicorp/terraform/helper/schema"
	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/methods"
	"github.com/vmware/govmomi/vim25/types"
	"golang.org/x/net/context"
)

func resourceVSphereDistributedVirtualPortgroup() *schema.Resource {
	return &schema.Resource{
		Create: resourceVSphereDistributedVirtualPortgroupCreate,
		Read:   resourceVSphereDistributedVirtualPortgroupRead,
		Delete: resourceVSphereDistributedVirtualPortgroupDelete,

		Schema: map[string]*schema.Schema{
			"datacenter": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
			"name": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
			"dvs_name": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
		},
	}
}

func resourceVSphereDistributedVirtualPortgroupCreate(d *schema.ResourceData, meta interface{}) error {

	client := meta.(*govmomi.Client)
	dvs_name := d.Get("dvs_name").(string)
	name := d.Get("name").(string)

	dc, err := getDatacenter(client, d.Get("datacenter").(string))
	if err != nil {
		return err
	}

	finder := find.NewFinder(client.Client, true)
	finder = finder.SetDatacenter(dc)

	df, err := dc.Folders(context.TODO())
	if err != nil {
		return fmt.Errorf("error %s", err)
	}

	f := df.NetworkFolder

	ns, err := finder.Network(context.TODO(), dvs_name)
	if err != nil {
		return fmt.Errorf("%s", err)
	}
	var dvs *object.DistributedVirtualSwitch
	for _, n := range ns {
		if n.Reference().Type == "DistributedVirtualSwitch" || n.Reference().Type == "VmwareDistributedVirtualSwitch" {
			n := object.NewDistributedVirtualSwitch(client.Client, n.Reference())
			dvsn, _ := n.Common.ObjectName(context.TODO())
			log.Printf("[TRACE] Network Name: %s", dvsn)
			if dvsn == dvs_name {
				dvs = n
				break
			}
		}
	}
	if dvs == nil {
		return fmt.Errorf("distributed virtual switch not found.")
	}

	defaultPortConfig := types.VMwareDVSPortSetting{}

	var dvpcs []types.DVPortgroupConfigSpec
	dvPortgroupConfigSpec := types.DVPortgroupConfigSpec{
		Name: "DVPortgroupTest",
		Type: "earlyBinding"}
	dvpcs = append(dvpcs, dvPortgroupConfigSpec)

	task, err := dvs.AddPortgroup(context.TODO(), dvpcs)
	if err != nil {
		log.Printf("%s", err)
	}

	err = task.Wait(context.TODO())
	if err != nil {
		log.Printf("%s", err)
	}

	d.SetId(name)

	return nil

}

func resourceVSphereDistributedVirtualPortgroupRead(d *schema.ResourceData, meta interface{}) error {
	_, err := dvpgExists(d, name)
	if err != nil {
		d.SetId("")
	}

	return nil
}

func dvpgExists(d *schema.ResourceData, meta interface{}) (object.NetworkReference, error) {
	client := meta.(*govmomi.Client)
	name := d.Get("name").(string)

	dc, err := getDatacenter(client, d.Get("datacenter").(string))
	if err != nil {
		return nil, err
	}

	finder := find.NewFinder(client.Client, true)
	finder = finder.SetDatacenter(dc)

	dvpg, err := finder.Network(context.TODO(), name)
	return dvpg, err
}

func resourceVSphereDVPortgroupStateRefreshFunc(d *schema.ResourceData, meta interface{}) resource.StateRefreshFunc {
	return func() (interface{}, string, error) {
		log.Print("[TRACE] Refreshing distributed portgroup state")
		dvs, err := dvpgExists(d, meta)
		if err != nil {
			switch err.(type) {
			case *find.NotFoundError:
				log.Printf("[TRACE] Refreshing state. Distributed portgroup not found: %s", err)
				return nil, "InProgress", nil
			default:
				return nil, "Failed", err
			}
		}
		log.Print("[TRACE] Refreshing state. Distributed portgroup found")
		return dvs, "Created", nil
	}
}

func resourceVSphereDistributedVirtualPortgroupDelete(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*govmomi.Client)
	name := d.Get("name").(string)

	dc, err := getDatacenter(client, d.Get("datacenter").(string))
	if err != nil {
		return err
	}

	finder := find.NewFinder(client.Client, true)
	finder = finder.SetDatacenter(dc)

	dvpg, err := finder.Network(context.TODO(), name)
	if err != nil {
		return fmt.Errorf("%s", err)
	}

	req := &types.Destroy_Task{
		This: dvpg.Reference(),
	}

	_, err = methods.Destroy_Task(context.TODO(), client, req)
	if err != nil {
		return fmt.Errorf("%s", err)
	}

	// Wait for the distributed virtual switch resource to be destroyed
	stateConf := &resource.StateChangeConf{
		Pending:    []string{"Created"},
		Target:     []string{},
		Refresh:    resourceVSphereDVPortgroupStateRefreshFunc(d, meta),
		Timeout:    10 * time.Minute,
		MinTimeout: 3 * time.Second,
		Delay:      5 * time.Second,
	}

	_, err = stateConf.WaitForState()
	if err != nil {
		name := d.Get("name").(string)
		return fmt.Errorf("error waiting for the distributed portgroup (%s) to be deleted: %s", name, err)
	}

	return nil
}
