// Copyright 2026. Licensed under the Apache License, Version 2.0.

package controller

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"testing"

	keylineapi "github.com/The127/Keyline/api"
	keylineclient "github.com/The127/Keyline/client"
	"github.com/google/uuid"

	keylinev1alpha1 "github.com/keyline/keyline-operator/api/v1alpha1"
)

// fakeUserClient records Associate/Remove calls and lets tests inject errors.
// Only Associate/Remove are used by reconcileKeys; other interface methods
// panic if accidentally called.
type fakeUserClient struct {
	associateErr  map[string]error // keyed by kid
	removeErr     map[string]error
	associateKids []string
	removeKids    []string
}

func (f *fakeUserClient) Create(context.Context, keylineapi.CreateUserRequestDto) (keylineapi.CreateUserResponseDto, error) {
	panic("unused")
}

func (f *fakeUserClient) List(context.Context, keylineclient.ListUserParams) (keylineapi.PagedUsersResponseDto, error) {
	panic("unused")
}

func (f *fakeUserClient) Get(context.Context, uuid.UUID) (keylineapi.GetUserByIdResponseDto, error) {
	panic("unused")
}

func (f *fakeUserClient) Patch(context.Context, uuid.UUID, keylineapi.PatchUserRequestDto) error {
	panic("unused")
}

func (f *fakeUserClient) CreateServiceUser(context.Context, string) (uuid.UUID, error) {
	panic("unused")
}

func (f *fakeUserClient) AssociateServiceUserPublicKey(_ context.Context, _ uuid.UUID, dto keylineapi.AssociateServiceUserPublicKeyRequestDto) (keylineapi.AssociateServiceUserPublicKeyResponseDto, error) {
	kid := ""
	if dto.Kid != nil {
		kid = *dto.Kid
	}
	f.associateKids = append(f.associateKids, kid)
	if err := f.associateErr[kid]; err != nil {
		return keylineapi.AssociateServiceUserPublicKeyResponseDto{}, err
	}
	return keylineapi.AssociateServiceUserPublicKeyResponseDto{Kid: kid}, nil
}

func (f *fakeUserClient) RemoveServiceUserPublicKey(_ context.Context, _ uuid.UUID, kid string) error {
	f.removeKids = append(f.removeKids, kid)
	return f.removeErr[kid]
}

func newUserWithKeys(spec []keylinev1alpha1.ServiceUserPublicKey, managed []string) *keylinev1alpha1.KeylineUser {
	return &keylinev1alpha1.KeylineUser{
		Spec: keylinev1alpha1.KeylineUserSpec{
			PublicKeys: spec,
		},
		Status: keylinev1alpha1.KeylineUserStatus{
			UserId:        uuid.NewString(),
			ManagedKeyIds: slices.Clone(managed),
		},
	}
}

func pk(kid, pem string) keylinev1alpha1.ServiceUserPublicKey {
	return keylinev1alpha1.ServiceUserPublicKey{Kid: kid, PublicKeyPEM: pem}
}

func TestReconcileKeys_NoSpecNoStatus_NoCalls(t *testing.T) {
	r := &KeylineUserReconciler{}
	uc := &fakeUserClient{}
	user := newUserWithKeys(nil, nil)
	id := uuid.MustParse(user.Status.UserId)

	if _, err := r.reconcileKeys(context.Background(), uc, user, id); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(uc.associateKids)+len(uc.removeKids) != 0 {
		t.Fatalf("expected no calls, got associate=%v remove=%v", uc.associateKids, uc.removeKids)
	}
	if len(user.Status.ManagedKeyIds) != 0 {
		t.Fatalf("expected empty managed, got %v", user.Status.ManagedKeyIds)
	}
}

func TestReconcileKeys_AddsNewKids(t *testing.T) {
	r := &KeylineUserReconciler{}
	uc := &fakeUserClient{}
	user := newUserWithKeys(
		[]keylinev1alpha1.ServiceUserPublicKey{pk("a", "PEM-A"), pk("b", "PEM-B")},
		nil,
	)
	id := uuid.MustParse(user.Status.UserId)

	if _, err := r.reconcileKeys(context.Background(), uc, user, id); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	slices.Sort(uc.associateKids)
	if got, want := uc.associateKids, []string{"a", "b"}; !slices.Equal(got, want) {
		t.Fatalf("associate kids: got %v want %v", got, want)
	}
	if len(uc.removeKids) != 0 {
		t.Fatalf("expected no removes, got %v", uc.removeKids)
	}
	got := slices.Clone(user.Status.ManagedKeyIds)
	slices.Sort(got)
	if want := []string{"a", "b"}; !slices.Equal(got, want) {
		t.Fatalf("managed kids: got %v want %v", got, want)
	}
}

func TestReconcileKeys_SkipsAlreadyManaged(t *testing.T) {
	r := &KeylineUserReconciler{}
	uc := &fakeUserClient{}
	user := newUserWithKeys(
		[]keylinev1alpha1.ServiceUserPublicKey{pk("a", "PEM-A")},
		[]string{"a"},
	)
	id := uuid.MustParse(user.Status.UserId)

	if _, err := r.reconcileKeys(context.Background(), uc, user, id); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(uc.associateKids)+len(uc.removeKids) != 0 {
		t.Fatalf("expected no calls, got associate=%v remove=%v", uc.associateKids, uc.removeKids)
	}
}

func TestReconcileKeys_RemovesDroppedKids(t *testing.T) {
	r := &KeylineUserReconciler{}
	uc := &fakeUserClient{}
	user := newUserWithKeys(
		[]keylinev1alpha1.ServiceUserPublicKey{pk("a", "PEM-A")},
		[]string{"a", "b"},
	)
	id := uuid.MustParse(user.Status.UserId)

	if _, err := r.reconcileKeys(context.Background(), uc, user, id); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !slices.Equal(uc.removeKids, []string{"b"}) {
		t.Fatalf("remove kids: got %v want [b]", uc.removeKids)
	}
	if !slices.Equal(user.Status.ManagedKeyIds, []string{"a"}) {
		t.Fatalf("managed after remove: got %v want [a]", user.Status.ManagedKeyIds)
	}
}

func TestReconcileKeys_AddAndRemoveInSamePass(t *testing.T) {
	r := &KeylineUserReconciler{}
	uc := &fakeUserClient{}
	user := newUserWithKeys(
		[]keylinev1alpha1.ServiceUserPublicKey{pk("new", "PEM-NEW")},
		[]string{"old"},
	)
	id := uuid.MustParse(user.Status.UserId)

	if _, err := r.reconcileKeys(context.Background(), uc, user, id); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !slices.Equal(uc.associateKids, []string{"new"}) {
		t.Fatalf("associate: got %v want [new]", uc.associateKids)
	}
	if !slices.Equal(uc.removeKids, []string{"old"}) {
		t.Fatalf("remove: got %v want [old]", uc.removeKids)
	}
	if !slices.Equal(user.Status.ManagedKeyIds, []string{"new"}) {
		t.Fatalf("managed: got %v want [new]", user.Status.ManagedKeyIds)
	}
}

func TestReconcileKeys_RemoveTolerates404(t *testing.T) {
	r := &KeylineUserReconciler{}
	uc := &fakeUserClient{
		removeErr: map[string]error{
			"gone": keylineclient.ApiError{Code: http.StatusNotFound, Message: "not found"},
		},
	}
	user := newUserWithKeys(nil, []string{"gone"})
	id := uuid.MustParse(user.Status.UserId)

	if _, err := r.reconcileKeys(context.Background(), uc, user, id); err != nil {
		t.Fatalf("expected 404 to be swallowed, got %v", err)
	}
	if len(user.Status.ManagedKeyIds) != 0 {
		t.Fatalf("expected kid dropped from status after 404, got %v", user.Status.ManagedKeyIds)
	}
}

func TestReconcileKeys_RemoveNon404ErrorPreservesStatus(t *testing.T) {
	r := &KeylineUserReconciler{}
	uc := &fakeUserClient{
		removeErr: map[string]error{
			"boom": errors.New("server error"),
		},
	}
	user := newUserWithKeys(nil, []string{"boom", "other"})
	id := uuid.MustParse(user.Status.UserId)

	failure, err := r.reconcileKeys(context.Background(), uc, user, id)
	if err == nil {
		t.Fatalf("expected error")
	}
	if failure.reason != "RemoveKeyFailed" {
		t.Fatalf("reason: got %q want RemoveKeyFailed", failure.reason)
	}
	// Status must be unchanged so a retry still knows the kids are ours.
	if !slices.Equal(user.Status.ManagedKeyIds, []string{"boom", "other"}) {
		t.Fatalf("managed unchanged: got %v want [boom other]", user.Status.ManagedKeyIds)
	}
}

func TestReconcileKeys_AssociateErrorStopsAndReturnsReason(t *testing.T) {
	r := &KeylineUserReconciler{}
	uc := &fakeUserClient{
		associateErr: map[string]error{"a": errors.New("boom")},
	}
	user := newUserWithKeys(
		[]keylinev1alpha1.ServiceUserPublicKey{pk("a", "PEM-A")},
		nil,
	)
	id := uuid.MustParse(user.Status.UserId)

	failure, err := r.reconcileKeys(context.Background(), uc, user, id)
	if err == nil {
		t.Fatalf("expected error")
	}
	if failure.reason != "AssociateKeyFailed" {
		t.Fatalf("reason: got %q want AssociateKeyFailed", failure.reason)
	}
	// Kid must not have been recorded as managed since the call failed.
	if len(user.Status.ManagedKeyIds) != 0 {
		t.Fatalf("expected empty managed after failed associate, got %v", user.Status.ManagedKeyIds)
	}
}
