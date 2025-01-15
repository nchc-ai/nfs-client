/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/golang/glog"
	"sigs.k8s.io/sig-storage-lib-external-provisioner/v6/controller"

	otiai10 "github.com/otiai10/copy"
	v1 "k8s.io/api/core/v1"
	storage "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	provisionerNameKey = "PROVISIONER_NAME"
)

type nfsProvisioner struct {
	client kubernetes.Interface
	server string
	path   string
}

const (
	mountPath = "/persistentvolumes"
)

const (
	annCopyDate        = "nchc.ai/copy-data"
	annLinkDate        = "nchc.ai/link-data"
	annSrcPVCNamespace = "nchc.ai/src-pvc-namespace"
	annSrcPVCName      = "nchc.ai/src-pvc-name"
)

var _ controller.Provisioner = &nfsProvisioner{}

func (p *nfsProvisioner) Provision(ctx context.Context, options controller.ProvisionOptions) (*v1.PersistentVolume, controller.ProvisioningState, error) {
	if options.PVC.Spec.Selector != nil {
		return nil, controller.ProvisioningFinished, fmt.Errorf("claim Selector is not supported")
	}
	glog.V(4).Infof("nfs provisioner: VolumeOptions %v", options)

	pvcNamespace := options.PVC.Namespace
	pvcName := options.PVC.Name

	pvName := strings.Join([]string{pvcNamespace, pvcName, options.PVName}, "-")

	fullPath := filepath.Join(mountPath, pvName)
	glog.V(4).Infof("creating path %s", fullPath)

	isLinkData, isLinkDataFound := options.PVC.Annotations[annLinkDate]
	isCopyData, isCopyDataFound := options.PVC.Annotations[annCopyDate]

	islinkdata, _ := strconv.ParseBool(isLinkData)
	iscopydata, _ := strconv.ParseBool(isCopyData)

	// when we create symbolic link, no need to create folder
	if !(isLinkDataFound == true && islinkdata == true) {
		if err := os.MkdirAll(fullPath, 0777); err != nil {
			return nil, controller.ProvisioningFinished, errors.New("unable to create directory to provision new pv: " + err.Error())
		}
		os.Chmod(fullPath, 0777)
	}

	if (isCopyDataFound == true && iscopydata == true) || (isLinkDataFound == true && islinkdata == true) {
		srcPvcNS, srcPvcNsFound := options.PVC.Annotations[annSrcPVCNamespace]
		srcPvcName, srcPvcNameFound := options.PVC.Annotations[annSrcPVCName]

		if srcPvcNsFound == true && srcPvcNS != "" &&
			srcPvcNameFound == true && srcPvcName != "" {
			srcPVC, err := p.client.CoreV1().PersistentVolumeClaims(srcPvcNS).Get(ctx, srcPvcName, metav1.GetOptions{})
			if err != nil {
				glog.Warningf("Get pvc {%s/%s} fail: %s", srcPvcNS, srcPvcName, err.Error())
			} else {
				srcPVName := strings.Join([]string{srcPvcNS, srcPvcName, srcPVC.Spec.VolumeName}, "-")

				if islinkdata {
					glog.Infof("Create symbolic link from %s to %s", srcPVName, pvName)
					err = p.linkDirectory(srcPVName, pvName)
					if err != nil {
						glog.Warningf("error Create symbolic link: %s", err.Error())
					}
				}

				if iscopydata {
					glog.Infof("Copy backing folder data from %s to %s", srcPVName, pvName)
					err = p.copyDirectory(srcPVName, pvName)
					if err != nil {
						glog.Warningf("error copy dataset backing folder: %s", err.Error())
					}
				}
			}
		}
	}

	path := filepath.Join(p.path, pvName)

	pv := &v1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: options.PVName,
		},
		Spec: v1.PersistentVolumeSpec{
			PersistentVolumeReclaimPolicy: *options.StorageClass.ReclaimPolicy,
			AccessModes:                   options.PVC.Spec.AccessModes,
			MountOptions:                  options.StorageClass.MountOptions,
			Capacity: v1.ResourceList{
				v1.ResourceName(v1.ResourceStorage): options.PVC.Spec.Resources.Requests[v1.ResourceName(v1.ResourceStorage)],
			},
			PersistentVolumeSource: v1.PersistentVolumeSource{
				NFS: &v1.NFSVolumeSource{
					Server:   p.server,
					Path:     path,
					ReadOnly: false,
				},
			},
		},
	}
	return pv, controller.ProvisioningFinished, nil
}

func (p *nfsProvisioner) Delete(ctx context.Context, volume *v1.PersistentVolume) error {
	path := volume.Spec.PersistentVolumeSource.NFS.Path
	oldPath := filepath.Base(path)

	err := os.Chdir(mountPath)
	if err != nil {
		return err
	}

	var fileInfo os.FileInfo

	if fileInfo, err = os.Lstat(oldPath); os.IsNotExist(err) {
		glog.Warningf("path %s does not exist, deletion skipped", filepath.Join(mountPath, oldPath))
		return nil
	}

	if fileInfo.Mode()&os.ModeSymlink != 0 {
		return os.RemoveAll(oldPath)
	}

	// Get the storage class for this volume.
	storageClass, err := p.getClassForVolume(ctx, volume)
	if err != nil {
		return err
	}
	// Determine if the "archiveOnDelete" parameter exists.
	// If it exists and has a false value, delete the directory.
	// Otherwise, archive it.
	archiveOnDelete, exists := storageClass.Parameters["archiveOnDelete"]
	if exists {
		archiveBool, err := strconv.ParseBool(archiveOnDelete)
		if err != nil {
			return err
		}
		if !archiveBool {
			return os.RemoveAll(oldPath)
		}
	}

	archivePath := "archived-" + oldPath
	glog.V(4).Infof("archiving path %s to %s", filepath.Join(mountPath, oldPath), filepath.Join(mountPath, archivePath))
	return os.Rename(oldPath, archivePath)

}

// getClassForVolume returns StorageClass
func (p *nfsProvisioner) getClassForVolume(ctx context.Context, pv *v1.PersistentVolume) (*storage.StorageClass, error) {
	if p.client == nil {
		return nil, fmt.Errorf("Cannot get kube client")
	}
	className := pv.Spec.StorageClassName
	if className == "" {
		return nil, fmt.Errorf("Volume has no storage class")
	}
	class, err := p.client.StorageV1().StorageClasses().Get(ctx, className, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return class, nil
}

func (p *nfsProvisioner) copyDirectory(srcDir string, destDir string) error {
	err := otiai10.Copy(path.Join(mountPath, srcDir), path.Join(mountPath, destDir))
	return err
}

func (p *nfsProvisioner) linkDirectory(srcDir string, destDir string) error {
	err := os.Chdir(mountPath)
	if err != nil {
		return err
	}
	err = os.Symlink(srcDir, destDir)
	return err
}

func main() {
	flag.Parse()
	flag.Set("logtostderr", "true")

	server := os.Getenv("NFS_SERVER")
	if server == "" {
		glog.Fatal("NFS_SERVER not set")
	}
	path := os.Getenv("NFS_PATH")
	if path == "" {
		glog.Fatal("NFS_PATH not set")
	}
	provisionerName := os.Getenv(provisionerNameKey)
	if provisionerName == "" {
		glog.Fatalf("environment variable %s is not set! Please set it.", provisionerNameKey)
	}

	// Create an InClusterConfig and use it to create a client for the controller
	// to use to communicate with Kubernetes
	config, err := rest.InClusterConfig()
	if err != nil {
		glog.Fatalf("Failed to create config: %v", err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		glog.Fatalf("Failed to create client: %v", err)
	}

	// The controller needs to know what the server version is because out-of-tree
	// provisioners aren't officially supported until 1.5
	serverVersion, err := clientset.Discovery().ServerVersion()
	if err != nil {
		glog.Fatalf("Error getting server version: %v", err)
	}

	clientNFSProvisioner := &nfsProvisioner{
		client: clientset,
		server: server,
		path:   path,
	}
	// Start the provision controller which will dynamically provision efs NFS
	// PVs
	pc := controller.NewProvisionController(clientset, provisionerName, clientNFSProvisioner, serverVersion.GitVersion)
	pc.Run(context.Background())
}
