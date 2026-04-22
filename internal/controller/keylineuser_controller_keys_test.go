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
	"k8s.io/apimachinery/pkg/runtime"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	keylinev1alpha1 "github.com/keyline/keyline-operator/api/v1alpha1"
)

// fakeUserClient records all UserClient calls and lets tests inject responses
// or errors per method. Zero-valued responses are returned by default.
type fakeUserClient struct {
	// Programmable responses.
	getFn             func(uuid.UUID) (keylineapi.GetUserByIdResponseDto, error)
	listFn            func() (keylineapi.PagedUsersResponseDto, error)
	createServiceUser func(username string) (uuid.UUID, error)
	patchErr          error
	associateErr      map[string]error
	removeErr         map[string]error

	// Recorded calls.
	getIDs             []uuid.UUID
	listCalls          int
	createdUsers       []string
	patchedDisplayName []string
	associateKids      []string
	removeKids         []string
}

func (f *fakeUserClient) Create(context.Context, keylineapi.CreateUserRequestDto) (keylineapi.CreateUserResponseDto, error) {
	panic("unused")
}

func (f *fakeUserClient) List(_ context.Context, _ keylineclient.ListUserParams) (keylineapi.PagedUsersResponseDto, error) {
	f.listCalls++
	if f.listFn != nil {
		return f.listFn()
	}
	return keylineapi.PagedUsersResponseDto{}, nil
}

func (f *fakeUserClient) Get(_ context.Context, id uuid.UUID) (keylineapi.GetUserByIdResponseDto, error) {
	f.getIDs = append(f.getIDs, id)
	if f.getFn != nil {
		return f.getFn(id)
	}
	return keylineapi.GetUserByIdResponseDto{}, nil
}

func (f *fakeUserClient) Patch(_ context.Context, _ uuid.UUID, dto keylineapi.PatchUserRequestDto) error {
	if dto.DisplayName != nil {
		f.patchedDisplayName = append(f.patchedDisplayName, *dto.DisplayName)
	}
	return f.patchErr
}

func (f *fakeUserClient) CreateServiceUser(_ context.Context, username string) (uuid.UUID, error) {
	f.createdUsers = append(f.createdUsers, username)
	if f.createServiceUser != nil {
		return f.createServiceUser(username)
	}
	return uuid.New(), nil
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

// --- reconcileWithClient tests ---
//
// These drive the controller flow end-to-end with a fake UserClient and a
// fake k8s client so we catch regressions in the flow wiring (not just in
// reconcileKeys). Regression-in-mind: an earlier version of the controller
// returned early after a successful Get, skipping reconcileKeys entirely,
// which meant spec.publicKeys changes were never applied on subsequent
// reconciles.

type reconcileHarness struct {
	r    *KeylineUserReconciler
	user *keylinev1alpha1.KeylineUser
	uc   *fakeUserClient
	sp   *statusPatcher
}

func newReconcileHarness(t *testing.T, user *keylinev1alpha1.KeylineUser, uc *fakeUserClient) *reconcileHarness {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := keylinev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("adding scheme: %v", err)
	}
	k8s := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(user).
		WithStatusSubresource(&keylinev1alpha1.KeylineUser{}).
		Build()
	r := &KeylineUserReconciler{Client: k8s, Scheme: scheme}
	sp := newStatusPatcher(k8s, user, &user.Status.Conditions)
	return &reconcileHarness{r: r, user: user, uc: uc, sp: sp}
}

func (h *reconcileHarness) reload(t *testing.T) *keylinev1alpha1.KeylineUser {
	t.Helper()
	var out keylinev1alpha1.KeylineUser
	if err := h.r.Get(context.Background(), k8sclient.ObjectKeyFromObject(h.user), &out); err != nil {
		t.Fatalf("reloading user: %v", err)
	}
	return &out
}

func newUserForReconcile(spec keylinev1alpha1.KeylineUserSpec, status keylinev1alpha1.KeylineUserStatus) *keylinev1alpha1.KeylineUser {
	u := &keylinev1alpha1.KeylineUser{}
	u.Name = "u"
	u.Namespace = "default"
	u.Spec = spec
	u.Status = status
	return u
}

// ReconcileWithClient_UserMissingCreatesAndAssociatesKeys covers the cold
// start: nothing in status, List returns empty, CreateServiceUser issues an
// id, keys are then associated.
func TestReconcileWithClient_UserMissingCreatesAndAssociatesKeys(t *testing.T) {
	createdID := uuid.New()
	uc := &fakeUserClient{
		createServiceUser: func(string) (uuid.UUID, error) { return createdID, nil },
	}
	user := newUserForReconcile(keylinev1alpha1.KeylineUserSpec{
		Username:   "svc",
		PublicKeys: []keylinev1alpha1.ServiceUserPublicKey{pk("k1", "PEM-1")},
	}, keylinev1alpha1.KeylineUserStatus{})
	h := newReconcileHarness(t, user, uc)

	if _, err := h.r.reconcileWithClient(context.Background(), uc, user, h.sp); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if uc.listCalls != 1 {
		t.Errorf("list calls: got %d want 1", uc.listCalls)
	}
	if got, want := uc.createdUsers, []string{"svc"}; !slices.Equal(got, want) {
		t.Errorf("createdUsers: got %v want %v", got, want)
	}
	if got, want := uc.associateKids, []string{"k1"}; !slices.Equal(got, want) {
		t.Errorf("associateKids: got %v want %v", got, want)
	}

	persisted := h.reload(t)
	if persisted.Status.UserId != createdID.String() {
		t.Errorf("status.UserId: got %q want %q", persisted.Status.UserId, createdID.String())
	}
	if got, want := persisted.Status.ManagedKeyIds, []string{"k1"}; !slices.Equal(got, want) {
		t.Errorf("managedKeyIds: got %v want %v", got, want)
	}
}

// ReconcileWithClient_ExistingUserRemovesDroppedKid is the regression test:
// the user already exists (Get returns it), spec drops a kid, and the
// reconciler MUST remove that kid. Previously the Get branch short-circuited
// to setReady, so the kid lingered in Keyline and in status forever.
func TestReconcileWithClient_ExistingUserRemovesDroppedKid(t *testing.T) {
	existingID := uuid.New()
	uc := &fakeUserClient{
		getFn: func(uuid.UUID) (keylineapi.GetUserByIdResponseDto, error) {
			return keylineapi.GetUserByIdResponseDto{}, nil
		},
	}
	user := newUserForReconcile(keylinev1alpha1.KeylineUserSpec{
		Username:   "svc",
		PublicKeys: []keylinev1alpha1.ServiceUserPublicKey{pk("keep", "PEM-KEEP")},
	}, keylinev1alpha1.KeylineUserStatus{
		UserId:        existingID.String(),
		ManagedKeyIds: []string{"keep", "drop"},
	})
	h := newReconcileHarness(t, user, uc)

	if _, err := h.r.reconcileWithClient(context.Background(), uc, user, h.sp); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(uc.getIDs) != 1 {
		t.Errorf("expected exactly one Get call, got %d", len(uc.getIDs))
	}
	if uc.listCalls != 0 {
		t.Errorf("expected no List call (user id resolved via Get), got %d", uc.listCalls)
	}
	if len(uc.createdUsers) != 0 {
		t.Errorf("expected no CreateServiceUser, got %v", uc.createdUsers)
	}
	if got, want := uc.removeKids, []string{"drop"}; !slices.Equal(got, want) {
		t.Errorf("removeKids: got %v want %v", got, want)
	}

	persisted := h.reload(t)
	if got, want := persisted.Status.ManagedKeyIds, []string{"keep"}; !slices.Equal(got, want) {
		t.Errorf("managedKeyIds after remove: got %v want %v", got, want)
	}
}

// ReconcileWithClient_ExistingUserAddsNewKid mirrors the above but for the
// add direction: user exists, spec gains a kid, reconciler must associate it.
func TestReconcileWithClient_ExistingUserAddsNewKid(t *testing.T) {
	existingID := uuid.New()
	uc := &fakeUserClient{
		getFn: func(uuid.UUID) (keylineapi.GetUserByIdResponseDto, error) {
			return keylineapi.GetUserByIdResponseDto{}, nil
		},
	}
	user := newUserForReconcile(keylinev1alpha1.KeylineUserSpec{
		Username: "svc",
		PublicKeys: []keylinev1alpha1.ServiceUserPublicKey{
			pk("keep", "PEM-KEEP"),
			pk("new", "PEM-NEW"),
		},
	}, keylinev1alpha1.KeylineUserStatus{
		UserId:        existingID.String(),
		ManagedKeyIds: []string{"keep"},
	})
	h := newReconcileHarness(t, user, uc)

	if _, err := h.r.reconcileWithClient(context.Background(), uc, user, h.sp); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got, want := uc.associateKids, []string{"new"}; !slices.Equal(got, want) {
		t.Errorf("associateKids: got %v want %v", got, want)
	}
	if len(uc.removeKids) != 0 {
		t.Errorf("unexpected removes: %v", uc.removeKids)
	}

	persisted := h.reload(t)
	sorted := slices.Clone(persisted.Status.ManagedKeyIds)
	slices.Sort(sorted)
	if want := []string{"keep", "new"}; !slices.Equal(sorted, want) {
		t.Errorf("managedKeyIds: got %v want %v", sorted, want)
	}
}

// ReconcileWithClient_StaleUserIdFallsBackToList covers the case where
// status.userId is set but the user was deleted out of band. Get returns 404,
// the reconciler clears the id, falls back to List, finds the user by
// username, and continues without creating a duplicate.
func TestReconcileWithClient_StaleUserIdFallsBackToList(t *testing.T) {
	staleID := uuid.New()
	foundID := uuid.New()
	uc := &fakeUserClient{
		getFn: func(uuid.UUID) (keylineapi.GetUserByIdResponseDto, error) {
			return keylineapi.GetUserByIdResponseDto{}, keylineclient.ApiError{Code: http.StatusNotFound, Message: "not found"}
		},
		listFn: func() (keylineapi.PagedUsersResponseDto, error) {
			return keylineapi.PagedUsersResponseDto{
				Items: []keylineapi.ListUsersResponseDto{
					{Id: foundID, Username: "svc", IsServiceUser: true},
				},
			}, nil
		},
	}
	user := newUserForReconcile(keylinev1alpha1.KeylineUserSpec{
		Username: "svc",
	}, keylinev1alpha1.KeylineUserStatus{UserId: staleID.String()})
	h := newReconcileHarness(t, user, uc)

	if _, err := h.r.reconcileWithClient(context.Background(), uc, user, h.sp); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if uc.listCalls != 1 {
		t.Errorf("expected List fallback, got %d calls", uc.listCalls)
	}
	if len(uc.createdUsers) != 0 {
		t.Errorf("expected no Create (user found via List), got %v", uc.createdUsers)
	}
	persisted := h.reload(t)
	if persisted.Status.UserId != foundID.String() {
		t.Errorf("status.UserId: got %q want %q", persisted.Status.UserId, foundID.String())
	}
}

// ReconcileWithClient_DisplayNamePatched verifies the display-name is pushed
// to Keyline once the user is known.
func TestReconcileWithClient_DisplayNamePatched(t *testing.T) {
	displayName := "Portal Provisioner"
	existingID := uuid.New()
	uc := &fakeUserClient{
		getFn: func(uuid.UUID) (keylineapi.GetUserByIdResponseDto, error) {
			return keylineapi.GetUserByIdResponseDto{}, nil
		},
	}
	user := newUserForReconcile(keylinev1alpha1.KeylineUserSpec{
		Username:    "svc",
		DisplayName: &displayName,
	}, keylinev1alpha1.KeylineUserStatus{UserId: existingID.String()})
	h := newReconcileHarness(t, user, uc)

	if _, err := h.r.reconcileWithClient(context.Background(), uc, user, h.sp); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got, want := uc.patchedDisplayName, []string{displayName}; !slices.Equal(got, want) {
		t.Errorf("patchedDisplayName: got %v want %v", got, want)
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
