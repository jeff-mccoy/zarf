package images

import (
	"strings"

	"github.com/defenseunicorns/zarf/src/config"
	"github.com/defenseunicorns/zarf/src/internal/k8s"
	"github.com/defenseunicorns/zarf/src/internal/message"
	"github.com/defenseunicorns/zarf/src/internal/utils"
	"github.com/google/go-containerregistry/pkg/crane"
)

func PushToZarfRegistry(imageTarballPath string, buildImageList []string) error {
	message.Debugf("images.PushToZarfRegistry(%s, %s)", imageTarballPath, buildImageList)

	registryUrl := ""
	if config.GetContainerRegistryInfo().InternalRegistry {
		// Establish a registry tunnel to send the images to the zarf registry
		tunnel := k8s.NewZarfTunnel()
		tunnel.Connect(k8s.ZarfRegistry, false)
		defer tunnel.Close()

		registryUrl = tunnel.Endpoint()
	} else {
		registryUrl = config.GetContainerRegistryInfo().URL

		// TODO @JPERRY: Do the same thing I did for the git-url in `src/internal/git/push.go#42` (better yet break this out into a util func)
		if strings.Contains(registryUrl, "svc.cluster.local") {
			tunnel, err := k8s.NewTunnelFromServiceURL(registryUrl)
			if err != nil {
				return err
			}

			tunnel.Connect("", false)
			defer tunnel.Close()
			registryUrl = tunnel.Endpoint() // TODO: @JEPRRY pre-pending "http://" will break this.. Try to understand why..
		}
	}

	spinner := message.NewProgressSpinner("Storing images in the zarf registry")
	defer spinner.Stop()

	pushOptions := config.GetCraneAuthOption(config.GetContainerRegistryInfo().PushUser, config.GetContainerRegistryInfo().PushPassword)
	message.Debugf("crane pushOptions = %#v", pushOptions)

	for _, src := range buildImageList {
		spinner.Updatef("Updating image %s", src)
		img, err := crane.LoadTag(imageTarballPath, src, config.GetCraneOptions()...)
		if err != nil {
			return err
		}

		offlineName := utils.SwapHost(src, registryUrl)
		if err = crane.Push(img, offlineName, pushOptions); err != nil {
			return err
		}
	}

	spinner.Success()
	return nil
}
