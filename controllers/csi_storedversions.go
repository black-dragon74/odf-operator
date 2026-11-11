/*
Copyright 2021 Red Hat OpenShift Data Foundation.

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

package controllers

import (
	"context"
	"fmt"
	"slices"

	"github.com/go-logr/logr"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	csiStorageVersion    = "v1"
	csiDeprecatedVersion = "v1alpha1"
)

// csiCRDNames are the CRDs owned by cephcsi-operator whose v1alpha1 version was
// removed in 5.0. status.storedVersions must no longer list v1alpha1 before OLM
// applies the 5.0 CRDs.
var csiCRDNames = []string{
	"cephconnections.csi.ceph.io",
	"clientprofilemappings.csi.ceph.io",
	"clientprofiles.csi.ceph.io",
	"drivers.csi.ceph.io",
	"operatorconfigs.csi.ceph.io",
}

//+kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions/status,verbs=get;update;patch

// removeDeprecatedCsiStoredVersion drops v1alpha1 from the storedVersions of the
// csi.ceph.io CRDs. It must run before the cephcsi-operator subscription is
// moved to a channel whose CRDs no longer declare v1alpha1, otherwise OLM's CRD
// upgrade safety check and the apiserver both reject the CRD update.
func (r *SubscriptionReconciler) removeDeprecatedCsiStoredVersion(ctx context.Context, logger logr.Logger) error {
	for _, crdName := range csiCRDNames {
		crd := &apiextensionsv1.CustomResourceDefinition{}
		if err := r.Reader.Get(ctx, client.ObjectKey{Name: crdName}, crd); err != nil {
			if apierrors.IsNotFound(err) {
				logger.Info("CRD not found, skipping storedVersions migration", "crd", crdName)
				continue
			}
			return fmt.Errorf("failed to get CRD %q: %w", crdName, err)
		}

		if storageVersion := crdStorageVersion(crd); storageVersion != csiStorageVersion {
			logger.Info("unexpected CRD storage version, skipping storedVersions migration",
				"crd", crdName, "storageVersion", storageVersion)
			continue
		}

		previous := append([]string(nil), crd.Status.StoredVersions...)
		storedVersions := make([]string, 0, len(previous))
		pruned := false
		for _, version := range previous {
			if version == csiDeprecatedVersion {
				pruned = true
				continue
			}
			storedVersions = append(storedVersions, version)
		}
		if !pruned {
			continue
		}
		if !slices.Contains(storedVersions, csiStorageVersion) {
			storedVersions = append(storedVersions, csiStorageVersion)
		}

		crd.Status.StoredVersions = storedVersions
		if err := r.Client.Status().Update(ctx, crd); err != nil {
			return fmt.Errorf("failed to update storedVersions of CRD %q: %w", crdName, err)
		}
		logger.Info("removed deprecated version from CRD storedVersions",
			"crd", crdName, "previous", previous, "storedVersions", storedVersions)
	}

	return nil
}

func crdStorageVersion(crd *apiextensionsv1.CustomResourceDefinition) string {
	for _, version := range crd.Spec.Versions {
		if version.Storage {
			return version.Name
		}
	}
	return ""
}
