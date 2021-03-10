// Copyright 2020 Red Hat, Inc. and/or its affiliates
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kogitobuild

import (
	"errors"
	"github.com/kiegroup/kogito-cloud-operator/api"
	"github.com/kiegroup/kogito-cloud-operator/api/v1beta1"
	"github.com/kiegroup/kogito-cloud-operator/core/framework/util"
	"github.com/kiegroup/kogito-cloud-operator/core/operator"
	"github.com/kiegroup/kogito-cloud-operator/core/test"
	"github.com/kiegroup/kogito-cloud-operator/meta"
	buildv1 "github.com/openshift/api/build/v1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"reflect"
	"testing"
	"time"
)

func TestStatusChangeWhenConsecutiveErrorsOccur(t *testing.T) {
	instanceName := "quarkus-example"
	instance := &v1beta1.KogitoBuild{
		ObjectMeta: metav1.ObjectMeta{Name: instanceName, Namespace: t.Name()},
		Spec: v1beta1.KogitoBuildSpec{
			Type: api.RemoteSourceBuildType,
			GitSource: v1beta1.GitSource{
				URI: "https://github.com/kiegroup/kogito-examples/",
			},
			Runtime: api.QuarkusRuntimeType,
		},
	}
	cli := test.NewFakeClientBuilder().AddK8sObjects(instance).Build()
	err := errors.New("error")
	context := &operator.Context{
		Client: cli,
		Log:    test.TestLogger,
		Scheme: meta.GetRegisteredSchema(),
	}
	buildStatusHandler := NewStatusHandler(context)
	buildStatusHandler.HandleStatusChange(instance, err)

	test.AssertFetchMustExist(t, cli, instance)
	assert.Equal(t, 1, len(instance.Status.Conditions))
	assert.Equal(t, api.OperatorFailureReason, instance.Status.Conditions[0].Reason)

	// ops, same error?
	buildStatusHandler.HandleStatusChange(instance, err)
	// start queueing
	test.AssertFetchMustExist(t, cli, instance)
	assert.Equal(t, 2, len(instance.Status.Conditions))
	assert.Equal(t, api.OperatorFailureReason, instance.Status.Conditions[1].Reason)

	// kill that buffer
	for n := 0; n <= maxConditionsBuffer; n++ {
		buildStatusHandler.HandleStatusChange(instance, err)
	}
	test.AssertFetchMustExist(t, cli, instance)
	assert.Len(t, instance.Status.Conditions, maxConditionsBuffer)
}

func TestStatusChangeWhenBuildsAreRunning(t *testing.T) {
	instanceName := "quarkus-example"
	instance := &v1beta1.KogitoBuild{
		ObjectMeta: metav1.ObjectMeta{Name: instanceName, Namespace: t.Name()},
		Spec: v1beta1.KogitoBuildSpec{
			Type: api.RemoteSourceBuildType,
			GitSource: v1beta1.GitSource{
				URI: "https://github.com/kiegroup/kogito-examples/",
			},
			Runtime: api.QuarkusRuntimeType,
		},
	}
	cli := test.NewFakeClientBuilder().OnOpenShift().AddK8sObjects(instance).Build()
	context := &operator.Context{
		Client: cli,
		Log:    test.TestLogger,
		Scheme: meta.GetRegisteredSchema(),
	}
	deltaProcessor := &deltaProcessor{Context: context, build: instance}
	manager := deltaProcessor.getBuildManager()
	requested, err := manager.GetRequestedResources()
	assert.NoError(t, err)

	buildConfigs := requested[reflect.TypeOf(buildv1.BuildConfig{})]
	assert.Len(t, buildConfigs, 2)

	builds := []buildv1.Build{
		{
			ObjectMeta: metav1.ObjectMeta{
				CreationTimestamp: metav1.NewTime(time.Now().Add(time.Hour * 1)),
				Labels:            buildConfigs[0].GetLabels(),
				Namespace:         t.Name(),
				Name:              buildConfigs[0].GetName() + "-" + util.RandomSuffix(),
			},
			Spec: buildv1.BuildSpec{},
			Status: buildv1.BuildStatus{
				Phase:   buildv1.BuildPhasePending,
				Reason:  "",
				Message: "Hello!",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				CreationTimestamp: metav1.NewTime(time.Now().Add(time.Hour * 2)),
				Labels:            buildConfigs[0].GetLabels(),
				Namespace:         t.Name(),
				Name:              buildConfigs[0].GetName() + "-" + util.RandomSuffix(),
			},
			Spec: buildv1.BuildSpec{},
			Status: buildv1.BuildStatus{
				Phase:   buildv1.BuildPhaseNew,
				Reason:  "",
				Message: "Running!",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				CreationTimestamp: metav1.NewTime(time.Now().Add(time.Hour * 3)),
				Labels:            buildConfigs[0].GetLabels(),
				Namespace:         t.Name(),
				Name:              buildConfigs[0].GetName() + "-" + util.RandomSuffix(),
			},
			Spec: buildv1.BuildSpec{},
			Status: buildv1.BuildStatus{
				Phase:   buildv1.BuildPhaseCancelled,
				Reason:  "",
				Message: "Complete!",
			},
		},
	}
	buildObjs := test.ToRuntimeObjects(buildConfigs...)
	for _, b := range builds {
		buildObjs = append(buildObjs, b.DeepCopy())
	}
	var k8sObjs []runtime.Object
	k8sObjs = append(k8sObjs, buildObjs...)
	k8sObjs = append(k8sObjs, instance)

	// recreating the Client with our objects to make sure that the BCs will be there
	cli = test.NewFakeClientBuilder().AddK8sObjects(k8sObjs...).AddBuildObjects(buildObjs...).Build()
	err = nil
	context1 := &operator.Context{
		Client: cli,
		Log:    test.TestLogger,
		Scheme: meta.GetRegisteredSchema(),
	}
	buildStatusHandler := NewStatusHandler(context1)
	buildStatusHandler.HandleStatusChange(instance, err)
	test.AssertFetchMustExist(t, cli, instance)
	assert.Len(t, instance.Status.Conditions, 1)
	// only the younger
	assert.Equal(t, api.KogitoBuildFailure, instance.Status.Conditions[0].Type)
	assert.Equal(t, builds[len(builds)-1].Name, instance.Status.LatestBuild)
	assert.Len(t, instance.Status.Builds.Cancelled, 1)
	assert.Len(t, instance.Status.Builds.New, 1)
	assert.Len(t, instance.Status.Builds.Pending, 1)
}