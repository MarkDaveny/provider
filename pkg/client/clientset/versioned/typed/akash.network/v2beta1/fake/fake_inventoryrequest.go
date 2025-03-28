/*
Copyright The Akash Network Authors.

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

// Code generated by client-gen. DO NOT EDIT.

package fake

import (
	v2beta1 "github.com/akash-network/provider/pkg/apis/akash.network/v2beta1"
	akashnetworkv2beta1 "github.com/akash-network/provider/pkg/client/applyconfiguration/akash.network/v2beta1"
	typedakashnetworkv2beta1 "github.com/akash-network/provider/pkg/client/clientset/versioned/typed/akash.network/v2beta1"
	gentype "k8s.io/client-go/gentype"
)

// fakeInventoryRequests implements InventoryRequestInterface
type fakeInventoryRequests struct {
	*gentype.FakeClientWithListAndApply[*v2beta1.InventoryRequest, *v2beta1.InventoryRequestList, *akashnetworkv2beta1.InventoryRequestApplyConfiguration]
	Fake *FakeAkashV2beta1
}

func newFakeInventoryRequests(fake *FakeAkashV2beta1) typedakashnetworkv2beta1.InventoryRequestInterface {
	return &fakeInventoryRequests{
		gentype.NewFakeClientWithListAndApply[*v2beta1.InventoryRequest, *v2beta1.InventoryRequestList, *akashnetworkv2beta1.InventoryRequestApplyConfiguration](
			fake.Fake,
			"",
			v2beta1.SchemeGroupVersion.WithResource("inventoryrequests"),
			v2beta1.SchemeGroupVersion.WithKind("InventoryRequest"),
			func() *v2beta1.InventoryRequest { return &v2beta1.InventoryRequest{} },
			func() *v2beta1.InventoryRequestList { return &v2beta1.InventoryRequestList{} },
			func(dst, src *v2beta1.InventoryRequestList) { dst.ListMeta = src.ListMeta },
			func(list *v2beta1.InventoryRequestList) []*v2beta1.InventoryRequest {
				return gentype.ToPointerSlice(list.Items)
			},
			func(list *v2beta1.InventoryRequestList, items []*v2beta1.InventoryRequest) {
				list.Items = gentype.FromPointerSlice(items)
			},
		),
		fake,
	}
}
