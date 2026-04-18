package controller

import (
	"fmt"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"
)

func ReconcileSuccess() (ctrl.Result, error) {
	return ctrl.Result{}, nil
}

func ReconcileError(err error) (ctrl.Result, error) {
	return ctrl.Result{}, err
}

func ReconcileErrorf(format string, args ...any) (ctrl.Result, error) {
	return ctrl.Result{}, fmt.Errorf(format, args...)
}

func ReconcileAfter(d time.Duration) (ctrl.Result, error) {
	return ctrl.Result{RequeueAfter: d}, nil
}
