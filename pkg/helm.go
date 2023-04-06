package pkg

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/cli/values"
	"helm.sh/helm/v3/pkg/downloader"
	"helm.sh/helm/v3/pkg/strvals"

	"github.com/gofrs/flock"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/repo"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var helmLogger = logf.Log.WithName("helm")
var settings *cli.EnvSettings

// RepoAdd adds repo with given name and url
func RepoAdd(name, url string) error {

	settings = cli.New()
	repoFile := settings.RepositoryConfig

	//Ensure the file directory exists as it is required for file locking
	err := os.MkdirAll(filepath.Dir(repoFile), os.ModePerm)
	if err != nil && !os.IsExist(err) {
		return err
	}

	// Acquire a file lock for process synchronization
	fileLock := flock.New(strings.Replace(repoFile, filepath.Ext(repoFile), ".lock", 1))
	lockCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	locked, err := fileLock.TryLockContext(lockCtx, time.Second)
	if err == nil && locked {
		defer fileLock.Unlock()
	}
	if err != nil {
		return err
	}

	b, err := ioutil.ReadFile(repoFile)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	var f repo.File
	if err := yaml.Unmarshal(b, &f); err != nil {
		return err
	}

	if f.Has(name) {
		// repository name already exist
		return nil
	}

	c := repo.Entry{
		Name: name,
		URL:  url,
	}

	r, err := repo.NewChartRepository(&c, getter.All(settings))
	if err != nil {
		return err
	}

	if _, err := r.DownloadIndexFile(); err != nil {
		err := errors.Wrapf(err, "looks like %q is not a valid chart repository or cannot be reached", url)
		return err
	}

	f.Update(&c)

	if err := f.WriteFile(repoFile, 0644); err != nil {
		return err
	}
	//fmt.Printf("%q has been added to your repositories\n", name)
	return nil
}

// RepoUpdate updates charts for all helm repos
func RepoUpdate() error {
	repoFile := settings.RepositoryConfig

	f, err := repo.LoadFile(repoFile)
	if os.IsNotExist(errors.Cause(err)) || len(f.Repositories) == 0 {
		//log.Fatal(errors.New("no repositories found. You must add one before updating"))
		return err
	}
	var repos []*repo.ChartRepository
	for _, cfg := range f.Repositories {
		r, err := repo.NewChartRepository(cfg, getter.All(settings))
		if err != nil {
			return err
		}
		repos = append(repos, r)
	}

	//fmt.Printf("Hang tight while we grab the latest from your chart repositories...\n")
	var wg sync.WaitGroup
	for _, re := range repos {
		wg.Add(1)
		go func(re *repo.ChartRepository) {
			defer wg.Done()
			if _, err := re.DownloadIndexFile(); err != nil {
				fmt.Printf("...Unable to get an update from the %q chart repository (%s):\n\t%s\n", re.Config.Name, re.Config.URL, err)
			}
			/*
				else {
					fmt.Printf("...Successfully got an update from the %q chart repository\n", re.Config.Name)
				}
			*/
		}(re)
	}
	wg.Wait()
	//fmt.Printf("Update Complete. ⎈ Happy Helming!⎈\n")
	return nil
}

//func InstallHelmChart(name, namespace, repo, chartName )

// InstallChart
func InstallChart(name, namespace, repo, chart, chartVersion string, args map[string]string) error {

	actionConfig, err := newHelmConfiguration(namespace)
	if err != nil {
		return err
	}
	client := action.NewInstall(actionConfig)

	/*
		if client.Version == "" && client.Devel {
			client.Version = ">0.0.0-0"
		}
	*/

	client.Version = chartVersion
	//name, chart, err := client.NameAndChart(args)
	client.ReleaseName = name
	cp, err := client.ChartPathOptions.LocateChart(fmt.Sprintf("%s/%s", repo, chart), settings)
	if err != nil {
		return err
	}

	helmDebugLog("CHART PATH: %s\n", cp)

	p := getter.All(settings)
	valueOpts := &values.Options{}
	vals, err := valueOpts.MergeValues(p)
	if err != nil {
		return err
	}

	// Add args
	if err := strvals.ParseInto(args["set"], vals); err != nil {
		log.Fatal(errors.Wrap(err, "failed parsing --set data"))
		return err
	}

	// Check chart dependencies to make sure all are present in /charts
	chartRequested, err := loader.Load(cp)
	if err != nil {
		return err
	}

	validInstallableChart, err := isChartInstallable(chartRequested)
	if !validInstallableChart {
		return err
	}

	if req := chartRequested.Metadata.Dependencies; req != nil {
		// If CheckDependencies returns an error, we have unfulfilled dependencies.
		// As of Helm 2.4.0, this is treated as a stopping condition:
		// https://github.com/helm/helm/issues/2209
		if err := action.CheckDependencies(chartRequested, req); err != nil {
			if client.DependencyUpdate {
				man := &downloader.Manager{
					Out:              os.Stdout,
					ChartPath:        cp,
					Keyring:          client.ChartPathOptions.Keyring,
					SkipUpdate:       false,
					Getters:          p,
					RepositoryConfig: settings.RepositoryConfig,
					RepositoryCache:  settings.RepositoryCache,
				}
				if err := man.Update(); err != nil {
					return err
				}
			} else {
				return err
			}
		}
	}

	client.Namespace = namespace
	_, err = client.Run(chartRequested, vals)
	if err != nil {
		return err
	}
	return nil
}

// UninstallHelmChart uninstalls the given Helm chart name
func UninstallHelmChart(namespace string, releaseName string) error {
	// Uninstall Helm chart
	actionConfig, err := newHelmConfiguration(namespace)
	if err != nil {
		return err
	}
	uninstall := action.NewUninstall(actionConfig)
	response, err := uninstall.Run(releaseName)
	if err != nil {
		helmLogger.Error(err, "Failed to uninstall Helm release", "ReleaseName", releaseName)
		return err
	}

	helmLogger.Info("Successfully uninstall Helm release", "ReleaseName", response.Release.Name)

	return nil
}

func isChartInstallable(ch *chart.Chart) (bool, error) {
	switch ch.Metadata.Type {
	case "", "application":
		return true, nil
	}
	return false, errors.Errorf("%s charts are not installable", ch.Metadata.Type)
}

// NewHelmConfiguration creates a new Helm Configuration object under the given namespace
func newHelmConfiguration(namespace string) (*action.Configuration, error) {
	settings = cli.New()
	actionConfig := new(action.Configuration)
	err := actionConfig.Init(settings.RESTClientGetter(), namespace, os.Getenv("HELM_DRIVER"), helmDebugLog)

	return actionConfig, err
}

// helmDebugLog returns a logger that writes debug strings
func helmDebugLog(format string, v ...interface{}) {
	debugMsg := fmt.Sprintf(format, v...)
	helmLogger.Info(debugMsg)
}
