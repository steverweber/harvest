package options

import (
	"flag"
	"fmt"
	"os"
    "path"
    "strings"
)

type Options struct {
    Poller      string
    Daemon      bool
    Config      string
    Path        string
    LogLevel    int
    Debug       bool
    Version     string
    collectors  string
    objects     string
    Collectors   []string
    Objects     []string
    Hostname    string
}

func (o *Options) String() string {
    x := []string {
        fmt.Sprintf("%s= %s\n", "Poller", o.Poller),
        fmt.Sprintf("%s = %v\n", "Daemon", o.Daemon),
        fmt.Sprintf("%s = %s\n", "Config", o.Config),
        fmt.Sprintf("%s = %s\n", "Path", o.Path),
        fmt.Sprintf("%s = %s\n", "Hostname", o.Hostname),
        fmt.Sprintf("%s = %d\n", "LogLevel", o.LogLevel),
        fmt.Sprintf("%s = %v\n", "Debug", o.Debug),
        fmt.Sprintf("%s= %s\n", "Version", o.Version),
        fmt.Sprintf("%s = %v\n", "Collectors", o.Collectors),
        fmt.Sprintf("%s = %v\n", "Objects", o.Objects),
    }
    return strings.Join(x, ", ")
}

func (o *Options) Print() {
    fmt.Println(o.String())
}

type stringArray struct {
    container *[]string
    description string
}


func (s *stringArray) Set(v string) error {
    *s.container = append(*s.container, v)
    return nil
}

func (s *stringArray) String() string {
    return s.description
}


func GetOpts() (*Options, string, error)  {
	var args Options
    var err error
    args = Options{}

    //fmt.Println("\n--------------------------------------------------------------------------------")
    //fmt.Println(os.Args)
    //fmt.Println("--------------------------------------------------------------------------------\n")

    
    flag.StringVar(&args.Poller, "poller", "",
        "Poller name as defined in config")
    flag.BoolVar(&args.Daemon, "daemon", false,
        "Start as daemon")
    flag.StringVar(&args.Config, "config", "config.yaml",
        "Configuration file")
    flag.StringVar(&args.Path, "path", "",
        "Harvest installation directory")
    flag.IntVar(&args.LogLevel, "loglevel", 2,
        "logging level, index of: trace, debug, info, warning, error, critical")
    flag.BoolVar(&args.Debug, "debug", false,
        "Debug mode, no data will be exported")

    //collectors := stringArray{&args.Collectors, "list of collectors"}
    //objects := stringArray{&args.Objects, "list of objects"}

    flag.StringVar(&args.collectors, "collectors", "", "list of collectors to start (overrides config)")
    flag.StringVar(&args.objects, "objects", "", "list of collector objects to start (overrides config)")

    flag.Parse()

    if args.collectors != "" {
        args.Collectors = strings.Split(args.collectors, ",")
    }

    if args.objects != "" {
        args.Objects = strings.Split(args.objects, ",")
    }

    if args.Poller == "" {
        //fmt.Println("Missing required argument: poller")
        flag.PrintDefaults()
        os.Exit(1)
    }
    if args.Path == "" {
        var cwd string
		cwd, _ = os.Getwd()
        if base := path.Base(cwd); base == "poller" {
            //fmt.Println("base=", base)
            cwd, _ = path.Split(cwd)
            //fmt.Println("=> ", cwd)
        }
		if base := path.Base(cwd); base == "src" {
            //fmt.Println("base=", base)
			cwd, _ = path.Split(cwd)
            //fmt.Println("=> ", cwd)
		}
		args.Path = cwd
    }

    
    hostname, _ := os.Hostname()
    args.Hostname = hostname
    args.Version = "2.0.1"

    return &args, args.Poller, err
}
