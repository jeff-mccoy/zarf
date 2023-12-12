// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2021-Present The Zarf Authors

// Package images provides functions for building and pushing images.
package images

import (
	"fmt"
	"net/http"

	"github.com/defenseunicorns/zarf/src/config"
	"github.com/defenseunicorns/zarf/src/pkg/cluster"
	"github.com/defenseunicorns/zarf/src/pkg/k8s"
	"github.com/defenseunicorns/zarf/src/pkg/message"
	"github.com/defenseunicorns/zarf/src/pkg/transform"
	"github.com/defenseunicorns/zarf/src/pkg/utils"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/logs"
	v1 "github.com/google/go-containerregistry/pkg/v1"
)

// PushToZarfRegistry pushes a provided image into the configured Zarf registry
// This function will optionally shorten the image name while appending a checksum of the original image name.
func (i *ImageConfig) PushToZarfRegistry() error {
	message.Debug("images.PushToZarfRegistry()")

	logs.Warn.SetOutput(&message.DebugWriter{})
	logs.Progress.SetOutput(&message.DebugWriter{})

	refInfoToImage := map[transform.Image]v1.Image{}
	var totalSize int64
	// Build an image list from the references
	for _, refInfo := range i.ImageList {
		img, err := utils.LoadOCIImage(i.ImagesPath, refInfo)
		if err != nil {
			return err
		}
		refInfoToImage[refInfo] = img
		imgSize, err := calcImgSize(img)
		if err != nil {
			return err
		}
		totalSize += imgSize
	}

	// If this is not a no checksum image push we will be pushing two images (the second will go faster as it checks the same layers)
	if !i.NoChecksum {
		totalSize = totalSize * 2
	}

	httpTransport := http.DefaultTransport.(*http.Transport).Clone()
	httpTransport.TLSClientConfig.InsecureSkipVerify = i.Insecure
	progressBar := message.NewProgressBar(totalSize, fmt.Sprintf("Pushing %d images to the zarf registry", len(i.ImageList)))
	defer progressBar.Stop()
	craneTransport := utils.NewTransport(httpTransport, progressBar)

	pushOptions := config.GetCraneOptions(i.Insecure, i.Architectures...)
	pushOptions = append(pushOptions, config.GetCraneAuthOption(i.RegInfo.PushUsername, i.RegInfo.PushPassword))
	pushOptions = append(pushOptions, crane.WithTransport(craneTransport))

	var (
		err         error
		tunnel      *k8s.Tunnel
		registryURL string
	)

	registryURL = i.RegInfo.Address

	c, _ := cluster.NewCluster()
	if c != nil {
		registryURL, tunnel, err = c.ConnectToZarfRegistryEndpoint(i.RegInfo)
		if err != nil {
			return err
		}
	}

	if tunnel != nil {
		defer tunnel.Close()
	}

	for refInfo, img := range refInfoToImage {
		refTruncated := message.Truncate(refInfo.Reference, 55, true)
		progressBar.UpdateTitle(fmt.Sprintf("Pushing %s", refTruncated))

		// If this is not a no checksum image push it for use with the Zarf agent
		if !i.NoChecksum {
			offlineNameCRC, err := transform.ImageTransformHost(registryURL, refInfo.Reference)
			if err != nil {
				return err
			}

			message.Debugf("crane.Push() %s:%s -> %s)", i.ImagesPath, refInfo.Reference, offlineNameCRC)

			if err = pushImageReference(img, offlineNameCRC, tunnel, pushOptions); err != nil {
				return err
			}
		}

		// To allow for other non-zarf workloads to easily see the images upload a non-checksum version
		// (this may result in collisions but this is acceptable for this use case)
		offlineName, err := transform.ImageTransformHostWithoutChecksum(registryURL, refInfo.Reference)
		if err != nil {
			return err
		}

		message.Debugf("crane.Push() %s:%s -> %s)", i.ImagesPath, refInfo.Reference, offlineName)

		if err = pushImageReference(img, offlineName, tunnel, pushOptions); err != nil {
			return err
		}
	}

	progressBar.Successf("Pushed %d images to the zarf registry", len(i.ImageList))

	return nil
}

func pushImageReference(img v1.Image, name string, tunnel *k8s.Tunnel, pushOptions []crane.Option) error {
	var err error
	craneErrChan := make(chan error)
	go func() {
		craneErrChan <- crane.Push(img, name, pushOptions...)
	}()
	if tunnel != nil {
		select {
		case err = <-craneErrChan:
			return err
		case err = <-tunnel.ErrChan():
			return err
		}
	}

	return <-craneErrChan
}

func calcImgSize(img v1.Image) (int64, error) {
	size, err := img.Size()
	if err != nil {
		return size, err
	}

	layers, err := img.Layers()
	if err != nil {
		return size, err
	}

	for _, layer := range layers {
		ls, err := layer.Size()
		if err != nil {
			return size, err
		}
		size += ls
	}

	return size, nil
}
