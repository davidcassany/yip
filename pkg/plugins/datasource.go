package plugins

import (
	"bufio"
	"fmt"
	"os"
	"os/user"
	"path"
	"strings"

	"github.com/pkg/errors"

	prv "github.com/davidcassany/linuxkit/pkg/metadata/providers"
	"github.com/mudler/yip/pkg/schema"
	log "github.com/sirupsen/logrus"
	"github.com/twpayne/go-vfs"
)

func DataSources(s schema.Stage, fs vfs.FS, console Console) error {
	var AvailableProviders = []prv.Provider{}

	if s.DataSources.Providers == nil {
		return nil
	}

	for _, dSProviders := range s.DataSources.Providers {
		switch {
		case dSProviders == "aws":
			AvailableProviders = append(AvailableProviders, prv.NewAWS())
		case dSProviders == "gcp":
			AvailableProviders = append(AvailableProviders, prv.NewGCP())
		case dSProviders == "hetzner":
			AvailableProviders = append(AvailableProviders, prv.NewHetzner())
		case dSProviders == "openstack":
			AvailableProviders = append(AvailableProviders, prv.NewOpenstack())
		case dSProviders == "packet":
			AvailableProviders = append(AvailableProviders, prv.NewPacket())
		case dSProviders == "scaleway":
			AvailableProviders = append(AvailableProviders, prv.NewScaleway())
		case dSProviders == "vultr":
			AvailableProviders = append(AvailableProviders, prv.NewVultr())
		case dSProviders == "digitalocean":
			AvailableProviders = append(AvailableProviders, prv.NewDigitalOcean())
		case dSProviders == "metaldata":
			AvailableProviders = append(AvailableProviders, prv.NewMetalData())
		case dSProviders == "cdrom":
			AvailableProviders = append(AvailableProviders, prv.ListCDROMs()...)
		case dSProviders == "file" && s.DataSources.Path != "":
			AvailableProviders = append(AvailableProviders, prv.FileProvider(s.DataSources.Path))
		}
	}

	if err := EnsureDirectories(schema.Stage{
		Directories: []schema.Directory{
			{
				Path:        prv.ConfigPath,
				Permissions: 0755,
				Owner:       os.Getuid(),
				Group:       os.Getgid(),
			},
		},
	}, fs, console); err != nil {
		return err
	}

	var p prv.Provider
	var userdata []byte
	var err error
	found := false
	for _, p = range AvailableProviders {
		if p.Probe() {
			userdata, err = p.Extract()
			if err != nil {
				log.Warningf("Failed extracting data from %s provider: %s", p.String(), err.Error())
			}
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("No metadata/userdata found. Bye")
	}

	err = writeDataFile(path.Join(prv.ConfigPath, "provider"), p.String(), fs, console)
	if err != nil {
		return err
	}

	basePath := prv.ConfigPath
	if s.DataSources.Path != "" && s.DataSources.Path != p.String() {
		basePath = s.DataSources.Path
	}

	if userdata != nil {
		if err := processUserData(basePath, userdata, fs, console); err != nil {
			return err
		}
	}

	//Apply the hostname if the provider extracted a hostname file
	if _, err := fs.Stat(path.Join(prv.ConfigPath, prv.Hostname)); err == nil {
		if err := processHostnameFile(fs, console); err != nil {
			return err
		}
	}

	//Apply the authorized_keys if the provider extracted a ssh/authorized_keys file
	if _, err := fs.Stat(path.Join(prv.ConfigPath, prv.SSH, authorizedFile)); err == nil {
		if err := processSSHFile(fs, console); err != nil {
			return err
		}
	}
	return nil
}

func processHostnameFile(fs vfs.FS, console Console) error {
	hostname, err := fs.ReadFile(path.Join(prv.ConfigPath, prv.Hostname))
	if err != nil {
		return err
	}

	return Hostname(schema.Stage{Hostname: string(hostname)}, fs, console)
}

func processSSHFile(fs vfs.FS, console Console) error {
	auth_keys, err := fs.ReadFile(path.Join(prv.ConfigPath, prv.SSH, authorizedFile))
	if err != nil {
		return err
	}
	var keys []string
	var line string
	usr, err := user.Current()
	if err != nil {
		return errors.Wrap(err, "could not get current user info")
	}

	scanner := bufio.NewScanner(strings.NewReader(string(auth_keys)))
	for scanner.Scan() {
		line = strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			keys = append(keys, line)
		}
	}
	return SSH(schema.Stage{SSHKeys: map[string][]string{usr.Username: keys}}, fs, console)
}

// If userdata can be parsed as a yipConfig file will create a <basePath>/userdata.yaml file
func processUserData(basePath string, data []byte, fs vfs.FS, console Console) error {
	if _, err := schema.Load(string(data), fs, nil, nil); err == nil {
		return writeDataFile(path.Join(basePath, "userdata.yaml"), string(data), fs, console)
	}

	log.Println("Could not unmarshall userdata, neither json or yaml")
	return writeDataFile(path.Join(basePath, "userdata"), string(data), fs, console)
}

func writeDataFile(filename string, content string, fs vfs.FS, console Console) error {
	err := EnsureFiles(schema.Stage{
		Files: []schema.File{
			{
				Path:        filename,
				Content:     content,
				Permissions: 0644,
				Owner:       os.Getuid(),
				Group:       os.Getgid(),
			},
		},
	}, fs, console)
	if err != nil {
		return errors.Wrap(err, "could not write data file")
	}
	return nil
}