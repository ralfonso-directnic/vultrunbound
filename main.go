package main

import (
  "context"
  "os"
  "fmt"
  "github.com/vultr/govultr/v2"
  "golang.org/x/oauth2"
  "flag"
  "log"
  "github.com/spf13/viper"
  "io/ioutil"
  "bytes"
  "encoding/gob"
  "os/exec"
  "strings"
  arr "github.com/adam-hanna/arrayOperations"
  "errors"

)

type DnsEntry struct{
  Name string
  ShortName string
  Address string
  Device string
}

var output string
var target string
var dnscache string
var short string


func main() {

  flag.StringVar(&output,"output","hosts","Output style, hosts, unbound-control")
  flag.StringVar(&short,"short","no","Omit short format ie just the part without the domain")
  flag.StringVar(&dnscache,"dnscache","./.dnscache","The dns cache file location - used for unbound diff")
  flag.StringVar(&target,"target","/etc/hosts","server or file target depending on context")
  ConfigSetup()


  flag.Parse()

  log.Println("Starting Up")


  if(viper.GetBool("debug")){

    log.SetFlags(log.LstdFlags | log.Lshortfile)

  }

  if(viper.GetString("output")!=""){

    output = viper.GetString("output")

  }

  if(viper.GetString("short")!=""){

    short = viper.GetString("short")

  }

  if(viper.GetString("dnscache")!=""){

    dnscache = viper.GetString("dnscache")

  }

  if(viper.GetString("target")!=""){

    target = viper.GetString("target")

  }


  log.Printf("Output Mode: %s\n",output)


  entries,err := MakeRequest()

  if(err!=nil) {
    log.Println(err)
  }

  switch(output){

  case "hosts":
    OutputHosts(entries)
  case "unbound-control":
    OutputUnboundControl(entries)

  }

}



func ConfigSetup(){


  viper.SetConfigType("json")
  viper.SetConfigName("config") // name of config file (without extension)
  viper.AddConfigPath("/etc/vultrunbound/")   // path to look for the config file in
  viper.AddConfigPath("$HOME/.vultrunbound")  // call multiple times to add many search paths
  viper.AddConfigPath(".")               // optionally look for config in the working directory
  err := viper.ReadInConfig() // Find and read the config  |file
  if err != nil { // Handle errors reading the config file
    panic(fmt.Errorf("Fatal error config file: %s \n", err))
  }

}


//* This method overwrites whatever is existing in hosts */

func OutputHosts(entries []DnsEntry){


  d:=DnsEntry{"localhost","localhost.localdomain localhost4 localhost4.localdomain4","127.0.0.1",""}

  entries = append(entries,d)

  d=DnsEntry{"localhost","localhost.localdomain localhost6 localhost6.localdomain6","127.0.0.1",""}

  entries = append(entries,d)

  str :=""

  for _,ent := range entries{


    if(short == "yes" ){

      str+=ent.Address+" "+ent.Name+"\n"

    }else{

      str+=ent.Address+" "+ent.ShortName+" "+ent.Name+"\n"

    }


  }
  dat := []byte(str)
  err := ioutil.WriteFile(target, dat, 0644)

  if(err!=nil){

    log.Fatal(err)
  }

}
//Its imporant we remove any zones that were once present, so we need to diff, meaning we keep a cache//

func OutputUnboundControl(entries []DnsEntry){

  //this block will give us a list of zones that need to be removed as they are present in the cache but no longer in the current list

  // entries = entries[:len(entries)-3]

  if fileExists(dnscache) {

    // do a diff, see if we need an update

    data,derr := ioutil.ReadFile(dnscache)

    if(derr!=nil){
      log.Fatal(derr)
    }

    buf := bytes.NewBuffer(data)
    dec := gob.NewDecoder(buf)

    var cache_entries []DnsEntry

    if err := dec.Decode(&cache_entries); err != nil {
      log.Fatal(err)
    }


    if len(cache_entries)>0 {



      remove,err := EntryDiff(entries, cache_entries)


      if(err!=nil){}

      if(len(remove)>0){

        for _,rem := range remove {

          cmd := "local_data_remove "+rem.Name

          state,err := UnboundCMD(cmd)

          if(err!=nil){

            log.Println(err)
          }else{

            log.Println(state,"-",cmd)
          }

        }

      }


    }


  }




  var buf bytes.Buffer

  enc := gob.NewEncoder(&buf)

  if err := enc.Encode(entries); err != nil {
    log.Fatal(err)
  }

  //save this!

  err2 := ioutil.WriteFile(dnscache, buf.Bytes(), 0644)

  if(err2!=nil){

    log.Fatal(err2)
  }


  for _,ent := range entries{


    cmd := "local_data_remove "+ent.Name

    state,err := UnboundCMD(cmd)

    if(err!=nil){

      log.Println(err)

    }else{

      log.Println(state,"-",cmd)
    }

    cmd = "local_data "+ent.Name+" A "+ent.Address

    state, err = UnboundCMD(cmd)

    if(err!=nil){

      log.Println(err)
    }else{

      log.Println(state,"-",cmd)
    }

  }



}


func UnboundCMD(in string) (string,error) {

  cmd := exec.Command("/usr/sbin/unbound-control",in)

  out, err := cmd.CombinedOutput()

  if err != nil {
    return "", err
  }

  return strings.Trim(string(out),"\n"),nil


}

func EntryDiff(a []DnsEntry,b []DnsEntry) ([]DnsEntry,error){


  var e []DnsEntry

  z, ok := arr.Difference(a,b)

  if !ok {
    return nil,nil
  }

  slice, ok := z.Interface().([]DnsEntry)

  if !ok {

    return e,errors.New("Unable to convert for diff")

  }

  return slice,nil



}

func MakeRequest() ([]DnsEntry,error) {

  var entries []DnsEntry

  apiKey := viper.GetString("vultr_key")

  config := &oauth2.Config{}
  ctx := context.Background()
  ts := config.TokenSource(ctx, &oauth2.Token{AccessToken: apiKey})
  vultrClient := govultr.NewClient(oauth2.NewClient(ctx, ts))

  // Optional changes
  _ = vultrClient.SetBaseURL("https://api.vultr.com")
  vultrClient.SetUserAgent("vultrunbound")
  vultrClient.SetRateLimit(500)

  listOptions := &govultr.ListOptions{PerPage:1}

  for {
    i, meta, err := vultrClient.Instance.List(context.Background(), listOptions)
    if err != nil {
      return nil, err
    }
    for _,it := range i {

      sname,shortname,shortnamep := ShortName(it.Label)


      d := DnsEntry{it.Label,shortname,it.MainIP,"eth0"}
      d2 := DnsEntry{sname,shortnamep,it.InternalIP,"eth1"}

      entries = append(entries,d)
      entries = append(entries,d2)

    }

    if meta.Links.Next == "" {
      break
    } else {
      listOptions.Cursor = meta.Links.Next
      continue
    }
  }


  return entries,nil
}

func ShortName(name string) (string, string,string){

  shortname_raw := strings.Split(name,".")

  shortname := name

  if(len(shortname_raw)>0){

    shortname = shortname_raw[0]

  }

  if(len(shortname_raw)>3){

    shortname = shortname_raw[0]+"."+shortname_raw[1]

  }

  shortname_rawp := shortname_raw

  shortnamep := shortname+"i"
  shortname_rawp[0] = shortname_rawp[0]+"i"

  sname := strings.Join(shortname_rawp,".");

  if(len(shortname_raw)>3){

    shortnamep = shortname_rawp[0]+"."+shortname_rawp[1]


  }

  return sname,shortname,shortnamep



}

func fileExists(filename string) bool {
  info, err := os.Stat(filename)
  if os.IsNotExist(err) {
    return false
  }
  return !info.IsDir()
}
