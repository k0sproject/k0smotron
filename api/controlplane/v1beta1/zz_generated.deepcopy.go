//go:build !ignore_autogenerated
// +build !ignore_autogenerated

/*
Copyright 2023.

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

// Code generated by controller-gen. DO NOT EDIT.

package v1beta1

import (
	"github.com/k0sproject/k0smotron/internal/cloudinit"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *K0sBootstrapConfigSpec) DeepCopyInto(out *K0sBootstrapConfigSpec) {
	*out = *in
	if in.Files != nil {
		in, out := &in.Files, &out.Files
		*out = make([]cloudinit.File, len(*in))
		copy(*out, *in)
	}
	if in.Args != nil {
		in, out := &in.Args, &out.Args
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.PreStartCommands != nil {
		in, out := &in.PreStartCommands, &out.PreStartCommands
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.PostStartCommands != nil {
		in, out := &in.PostStartCommands, &out.PostStartCommands
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new K0sBootstrapConfigSpec.
func (in *K0sBootstrapConfigSpec) DeepCopy() *K0sBootstrapConfigSpec {
	if in == nil {
		return nil
	}
	out := new(K0sBootstrapConfigSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *K0sControlPlane) DeepCopyInto(out *K0sControlPlane) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	out.Status = in.Status
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new K0sControlPlane.
func (in *K0sControlPlane) DeepCopy() *K0sControlPlane {
	if in == nil {
		return nil
	}
	out := new(K0sControlPlane)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *K0sControlPlane) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *K0sControlPlaneList) DeepCopyInto(out *K0sControlPlaneList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]K0sControlPlane, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new K0sControlPlaneList.
func (in *K0sControlPlaneList) DeepCopy() *K0sControlPlaneList {
	if in == nil {
		return nil
	}
	out := new(K0sControlPlaneList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *K0sControlPlaneList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *K0sControlPlaneMachineTemplate) DeepCopyInto(out *K0sControlPlaneMachineTemplate) {
	*out = *in
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	out.InfrastructureRef = in.InfrastructureRef
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new K0sControlPlaneMachineTemplate.
func (in *K0sControlPlaneMachineTemplate) DeepCopy() *K0sControlPlaneMachineTemplate {
	if in == nil {
		return nil
	}
	out := new(K0sControlPlaneMachineTemplate)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *K0sControlPlaneSpec) DeepCopyInto(out *K0sControlPlaneSpec) {
	*out = *in
	in.K0sConfigSpec.DeepCopyInto(&out.K0sConfigSpec)
	if in.MachineTemplate != nil {
		in, out := &in.MachineTemplate, &out.MachineTemplate
		*out = new(K0sControlPlaneMachineTemplate)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new K0sControlPlaneSpec.
func (in *K0sControlPlaneSpec) DeepCopy() *K0sControlPlaneSpec {
	if in == nil {
		return nil
	}
	out := new(K0sControlPlaneSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *K0sControlPlaneStatus) DeepCopyInto(out *K0sControlPlaneStatus) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new K0sControlPlaneStatus.
func (in *K0sControlPlaneStatus) DeepCopy() *K0sControlPlaneStatus {
	if in == nil {
		return nil
	}
	out := new(K0sControlPlaneStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *K0smotronControlPlane) DeepCopyInto(out *K0smotronControlPlane) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	out.Status = in.Status
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new K0smotronControlPlane.
func (in *K0smotronControlPlane) DeepCopy() *K0smotronControlPlane {
	if in == nil {
		return nil
	}
	out := new(K0smotronControlPlane)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *K0smotronControlPlane) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *K0smotronControlPlaneList) DeepCopyInto(out *K0smotronControlPlaneList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]K0smotronControlPlane, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new K0smotronControlPlaneList.
func (in *K0smotronControlPlaneList) DeepCopy() *K0smotronControlPlaneList {
	if in == nil {
		return nil
	}
	out := new(K0smotronControlPlaneList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *K0smotronControlPlaneList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *K0smotronControlPlaneStatus) DeepCopyInto(out *K0smotronControlPlaneStatus) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new K0smotronControlPlaneStatus.
func (in *K0smotronControlPlaneStatus) DeepCopy() *K0smotronControlPlaneStatus {
	if in == nil {
		return nil
	}
	out := new(K0smotronControlPlaneStatus)
	in.DeepCopyInto(out)
	return out
}