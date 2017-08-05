package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/kolo/xmlrpc"
)

type GandiAPI struct {
	key    string
	client *xmlrpc.Client
}

func NewGandiAPI(apiKey string) (*GandiAPI, error) {
	transport := http.Transport{
		ResponseHeaderTimeout: 60 * time.Second,
	}
	client, err := xmlrpc.NewClient("https://rpc.gandi.net/xmlrpc/", &transport)
	if err != nil {
		return nil, err
	}
	return &GandiAPI{
		key:    apiKey,
		client: client,
	}, nil
}

func (api *GandiAPI) GetZoneId(domain string) (int, error) {
	args := []interface{}{
		api.key,
		domain,
	}
	result := struct {
		ZoneId int `xmlrpc:"zone_id"`
	}{}
	err := api.client.Call("domain.info", args, &result)
	return result.ZoneId, err
}

type NewRecord struct {
	Type  string `xmlrpc:"type"`
	Name  string `xmlrpc:"name"`
	Value string `xmlrpc:"value"`
	TTL   int    `xmlrpc:"ttl"`
}

type Record struct {
	Id    string `xmlrpc:"id"`
	Type  string `xmlrpc:"type"`
	Name  string `xmlrpc:"name"`
	Value string `xmlrpc:"value"`
	TTL   int    `xmlrpc:"ttl"`
}

func (api *GandiAPI) GetZoneRecords(zoneId, version int) ([]Record, error) {
	args := []interface{}{
		api.key,
		zoneId,
		version,
	}
	result := []Record{}
	err := api.client.Call("domain.zone.record.list", args, &result)
	return result, err
}

func (api *GandiAPI) CopyZoneVersion(zoneId int) (int, error) {
	args := []interface{}{
		api.key,
		zoneId,
	}
	version := int(0)
	err := api.client.Call("domain.zone.version.new", args, &version)
	return version, err
}

func (api *GandiAPI) DeleteRecord(zoneId, version int, id string) (int, error) {
	// Documentation states "id" can be int or string, but a string value fails
	// if the related integer does not fit in 32-bits.
	intId, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return 0, err
	}
	args := []interface{}{
		api.key,
		zoneId,
		version,
		struct {
			Id int64 `xmlrpc:"id"`
		}{
			Id: intId,
		},
	}
	deleted := int(0)
	err = api.client.Call("domain.zone.record.delete", args, &deleted)
	return deleted, err
}

func (api *GandiAPI) AddRecord(zoneId, version int, record Record) (Record, error) {
	r := NewRecord{
		Type:  record.Type,
		Name:  record.Name,
		Value: record.Value,
		TTL:   record.TTL,
	}
	args := []interface{}{
		api.key,
		zoneId,
		version,
		r,
	}
	created := Record{}
	err := api.client.Call("domain.zone.record.add", args, &created)
	return created, err
}

func (api *GandiAPI) SetZoneVersion(zoneId, version int) error {
	args := []interface{}{
		api.key,
		zoneId,
		version,
	}
	ok := false
	err := api.client.Call("domain.zone.version.set", args, &ok)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("zone activation failed for unknown reason")
	}
	return nil
}

func (api *GandiAPI) DeleteZoneVersion(zoneId, version int) error {
	args := []interface{}{
		api.key,
		zoneId,
		version,
	}
	ok := false
	err := api.client.Call("domain.zone.version.delete", args, &ok)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("zone version deletion failed for unknown reason")
	}
	return nil
}

var (
	reIP = regexp.MustCompile(`\d+\.\d+\.\d+\.\d+`)
)

func getMyIP() (string, error) {
	client := http.Client{
		Timeout: 5 * time.Second,
	}
	rsp, err := client.Get("https://api.ipify.org?format=json")
	if err != nil {
		return "", err
	}
	if rsp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("http call failed with %d", rsp.StatusCode)
	}
	data, err := ioutil.ReadAll(rsp.Body)
	if err != nil {
		return "", err
	}
	ip := struct {
		IP string `json:"ip"`
	}{}
	err = json.Unmarshal(data, &ip)
	if err != nil {
		return "", err
	}
	if !reIP.MatchString(ip.IP) {
		return "", fmt.Errorf("does not look like an IPv4: %s", ip)
	}
	return ip.IP, nil
}

func updateRecords(api *GandiAPI, records []Record, zoneId, version int,
	ip string) error {

	records, err := api.GetZoneRecords(zoneId, version)
	if err != nil {
		return err
	}
	for _, r := range records {
		if r.Type != "A" || r.Value == ip {
			continue
		}
		r := r
		r.Value = ip
		fmt.Println("updating", r)
		n, err := api.DeleteRecord(zoneId, version, r.Id)
		if err != nil {
			return err
		}
		if n < 1 {
			return fmt.Errorf("no record deleted")
		}
		_, err = api.AddRecord(zoneId, version, r)
		if err != nil {
			return err
		}
	}
	return nil
}

func checkIP() error {
	flag.Usage = func() {
		fmt.Println(`Usage: gandi-dyn apikey mydomain.org

gandi-dyn fetches A records from Gandi for a domain using their API. If the
record value differs from the current IP obtained from a third-party service, a
new zone version is created, updated with the new address and activated.
`)
		os.Exit(1)
	}
	flag.Parse()
	if flag.NArg() < 1 {
		return fmt.Errorf("missing api-key argument")
	}
	if flag.NArg() < 2 {
		return fmt.Errorf("missing domain argument")
	}
	key := flag.Arg(0)
	domain := flag.Arg(1)

	ip, err := getMyIP()
	if err != nil {
		return err
	}
	fmt.Println(ip)
	api, err := NewGandiAPI(key)
	if err != nil {
		return err
	}
	zoneId, err := api.GetZoneId(domain)
	if err != nil {
		return err
	}
	fmt.Println("zoneid", zoneId)
	records, err := api.GetZoneRecords(zoneId, 0)
	if err != nil {
		return err
	}

	newRecords := []Record{}
	changed := false
	for _, r := range records {
		if r.Type == "A" && r.Value != ip {
			changed = true
			break
		}
	}
	if !changed {
		fmt.Println("unchanged ip")
		return nil
	}

	newVersion, err := api.CopyZoneVersion(zoneId)
	if err != nil {
		return err
	}
	err = updateRecords(api, newRecords, zoneId, newVersion, ip)
	if err != nil {
		fmt.Println("failed to apply records, deleting zone version")
		err2 := api.DeleteZoneVersion(zoneId, newVersion)
		if err2 != nil {
			fmt.Println("zone version deletion failed: %s", err)
		}
		return err
	}
	err = api.SetZoneVersion(zoneId, newVersion)
	if err != nil {
		fmt.Println("zone activation failed: %s", err)
		return err
	}
	// TODO: remove previous version?
	fmt.Println("zone activated")
	return fmt.Errorf("ip changed to %s", ip)
}

func main() {
	err := checkIP()
	if err != nil {
		fmt.Fprintf(os.Stderr, "err: %s\n", err)
		os.Exit(1)
	}
}
