package main

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/yo-l1982/imgpull/pkg/imgpull"
)

var (
	buildVer string = "SET BY MAKE FILE"
	buildDtm string = "SET BY MAKE FILE"
)

// optName is a unique option name. E.g. if "--user" is a supported
// cmdline option, then we would expect an optName = "user".
type optName string

// opt defines a command line option. The Name is intended to be used as its
// key in a map. Short and long are intended as (for example) -u and --user
// respectively. Value holds the value parsed from the actual command line and
// 'Dflt' is an optional default if no value is provided on the cmdline.
type opt struct {
	Name     optName
	Short    string
	Long     string
	Value    string
	Dflt     string
	IsSwitch bool
	Func     func(optMap)
}

// All the supported options
const (
	// positional param one - the image url
	imageOpt optName = "image"
	// positional param two - the tarball to save the image to
	destOpt optName = "dest"
	// e.g. --os linux
	osOpt optName = "os"
	// e.g. --arch amd64
	archOpt optName = "arch"
	// e.g. --ns docker.io
	namespaceOpt optName = "namespace"
	// e.g. --user jqpubli
	usernameOpt optName = "user"
	// e.g. --password mypassword
	passwordOpt optName = "password"
	// e.g. --scheme [http | https]
	schemeOpt optName = "scheme"
	// e.g. --cert /path/to/client-cert.pem
	certOpt optName = "cert"
	// e.g. --key /path/to/client-key.pem
	keyOpt optName = "key"
	// e.g. --cacert /path/to/ca.pem
	caOpt optName = "cacert"
	// e.g. --insecure
	insecureOpt optName = "insecure"
	// e.g. --manifest [list | image]
	manifestOpt optName = "manifest"
	// e.g. --version
	versionOpt optName = "version"
	// e.g. --help
	helpOpt optName = "help"
	// e.g. --parsed
	parsedOpt optName = "parsed"
)

// optMap holds the parsed command line
type optMap map[optName]opt

var usageText = `
Usage:

imgpull <image ref> <tar file> [-o|--os os] [-a|--arch arch] [-n|--ns namespace]
 [-u|--user username] [-p|--password password] [-s|--scheme scheme] [-c|--cert tls cert]
 [-k|--key tls key] [-x|--cacert tls ca cert] [-i|--insecure] [-m|--manifest type]
 [-v|--version] [-h|--help] [--parsed]

The image ref is required. Tar file is required if pulling a tarball. Everything else is
optional. The OS and architecture default to your system's values.

Example 1:

imgpull docker.io/hello-world:latest ./hello-world.latest.tar

The example pulls the image to hello-world.latest.tar in the working directory using the
operating system and architecture of the current system.

Example 2:

imgpull docker.io/hello-world:latest --manifest list

The example pulls the manifest list for hello-world:latest and displays it to the console.
`

// parseArgs parses and validates the command line parameters and options, returning them in a map.
// Only validations within the scope of the command line are validated. For example whether or not
// the URL is valid is not done here - that is determined by the Puller.
func parseArgs() (optMap, error) {
	opts := optMap{
		imageOpt:     {Name: imageOpt},
		destOpt:      {Name: destOpt},
		osOpt:        {Name: osOpt, Short: "o", Long: "os", Dflt: runtime.GOOS},
		archOpt:      {Name: archOpt, Short: "a", Long: "arch", Dflt: runtime.GOARCH},
		namespaceOpt: {Name: namespaceOpt, Short: "n", Long: "ns"},
		usernameOpt:  {Name: usernameOpt, Short: "u", Long: "user"},
		passwordOpt:  {Name: passwordOpt, Short: "p", Long: "password"},
		schemeOpt:    {Name: schemeOpt, Short: "s", Long: "scheme", Dflt: "https"},
		certOpt:      {Name: certOpt, Short: "c", Long: "cert"},
		keyOpt:       {Name: keyOpt, Short: "k", Long: "key"},
		caOpt:        {Name: caOpt, Short: "x", Long: "cacert"},
		insecureOpt:  {Name: insecureOpt, Short: "i", Long: "insecure", IsSwitch: true, Dflt: "false"},
		manifestOpt:  {Name: manifestOpt, Short: "m", Long: "manifest"},
		versionOpt:   {Name: versionOpt, Short: "v", Long: "version", IsSwitch: true, Func: showVersionAndExit},
		helpOpt:      {Name: helpOpt, Short: "h", Long: "help", IsSwitch: true, Func: showUsageAndExit},
		parsedOpt:    {Name: parsedOpt, Long: "parsed", IsSwitch: true, Func: showParsedAndExit},
	}
	for i := 1; i < len(os.Args); i++ {
		parsed := false
		for _, option := range opts {
			val, newi := getOptVal(option.Short, option.Long, option.IsSwitch, os.Args, i)
			if val != "" {
				if option.Func != nil {
					option.Func(opts)
				}
				if option.Value != "" {
					return opts, fmt.Errorf("option was specified more than once: %s", option.Name)
				}
				opts.setVal(option.Name, val)
				i = newi
				parsed = true
				break
			}
		}
		// handle positional params left to right
		if !parsed {
			if opts[imageOpt].Value == "" {
				opts.setVal(imageOpt, os.Args[i])
			} else if opts[destOpt].Value == "" {
				opts.setVal(destOpt, os.Args[i])
			} else {
				return opts, fmt.Errorf("unable to parse command line option: %s", os.Args[i])
			}
		}
	}
	if opts[manifestOpt].Value != "" {
		opts.setVal(manifestOpt, strings.ToLower(opts[manifestOpt].Value))
		if opts[manifestOpt].Value != "image" && opts[manifestOpt].Value != "list" {
			return opts, fmt.Errorf("invalid value %q for --manifest arg", opts[manifestOpt].Value)
		}
	}
	// need the image to pull
	if opts[imageOpt].Value == "" {
		return opts, errors.New("command line is missing image reference")
	}
	// maybe need the tarball to save it to
	if opts[destOpt].Value == "" && opts[manifestOpt].Value == "" {
		return opts, errors.New("command line is missing tarball to save to")
	}
	// apply any defaults if an override was not provided on the cmdline
	for _, option := range opts {
		if option.Value == "" && option.Dflt != "" {
			opts.setVal(option.Name, option.Dflt)
		}
	}
	return opts, nil
}

// pullerOptsFrom returns the passed map containing parsed args as a
// 'PullerOpts' struct.
func pullerOptsFrom(opts optMap) imgpull.PullerOpts {
	insecure, _ := strconv.ParseBool(opts.getVal(insecureOpt))
	return imgpull.PullerOpts{
		Url:       opts.getVal(imageOpt),
		Scheme:    opts.getVal(schemeOpt),
		OStype:    opts.getVal(osOpt),
		ArchType:  opts.getVal(archOpt),
		Namespace: opts.getVal(namespaceOpt),
		Username:  opts.getVal(usernameOpt),
		Password:  opts.getVal(passwordOpt),
		TlsCert:   opts.getVal(certOpt),
		TlsKey:    opts.getVal(keyOpt),
		CaCert:    opts.getVal(caOpt),
		Insecure:  insecure,
	}
}

// getOptVal gets an option value from a command line param. Several forms are supported:
//
//	--foo (this is a switch-style)
//	--foo bar
//	--foo=bar
//	-f (also switch-style)
//	-f bar
//	-fbar
//	-f=bar
//
// Compound short opts such as -fabc as shorthand for -f -a -b -c are not supported
// at this time. So '-fbar' is interpreted as '-f=bar'.
func getOptVal(short, long string, isswitch bool, args []string, i int) (string, int) {
	if short == "" && long == "" {
		// positional param
		return "", 0
	}
	if long != "" {
		opt := "--" + long
		// --foo
		if args[i] == opt && isswitch {
			return "true", i
		}
		// --foo bar
		if args[i] == opt && i < len(args)-1 {
			return args[i+1], i + 1
		}
		// --foo=bar
		opt += "="
		if strings.HasPrefix(args[i], opt) {
			return (args[i])[len(opt):], i
		}
	}
	if short != "" {
		opt := "-" + short
		// -f
		if args[i] == opt && isswitch {
			return "true", i
		}
		// -f bar
		if args[i] == opt && i < len(args)-1 {
			return args[i+1], i + 1
		}
		// -fbar
		if strings.HasPrefix(args[i], opt) && len(args[i]) > len(opt) {
			return (args[i])[len(opt):], i
		}
		// --f=bar
		opt += "="
		if strings.HasPrefix(args[i], opt) {
			return (args[i])[len(opt):], i
		}
	}
	return "", 0
}

// setVal sets the passed value in the options map for the passed option name.
func (m *optMap) setVal(name optName, value string) {
	opt := (*m)[name]
	opt.Value = value
	(*m)[name] = opt
}

// getVal gets the value from the options map for the passed option name.
func (m *optMap) getVal(name optName) string {
	return (*m)[name].Value
}

// showUsageAndExit prints usage instructions and terminates the program
// with a zero error code (will not return.)
func showUsageAndExit(opts optMap) {
	fmt.Println(usageText)
	os.Exit(0)
}

// showVersionAndExit prints version info and exits with a zero
// error code.
func showVersionAndExit(opts optMap) {
	fmt.Printf("imgpull version: %s build date: %s\n", buildVer, buildDtm)
	os.Exit(0)
}

// showParsedAndExit is a debug function that dumps the options map to the console
// in kind of an ugly format for troubleshooting.
func showParsedAndExit(opts optMap) {
	for name, opt := range opts {
		fmt.Printf("%s: %+v\n", name, opt)
	}
	os.Exit(0)
}
