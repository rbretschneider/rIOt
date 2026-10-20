//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/DesyncTheThird/rIOt/internal/agent/collectors"
	"github.com/DesyncTheThird/rIOt/internal/maintenance"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "maintenance:", err)
		os.Exit(1)
	}
}
func run(args []string) error {
	if len(args) != 0 {
		return errors.New("maintenance helper accepts no arguments")
	}
	if os.Geteuid() != 0 {
		return errors.New("maintenance helper requires root")
	}
	syscall.Umask(0027)
	lock, err := maintenance.AcquireLock()
	if err != nil {
		return err
	}
	defer lock.Close()
	store, err := maintenance.OpenStore()
	if err != nil {
		return err
	}
	defer store.Close()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	if err = maintenance.CheckPrerequisites(ctx); err != nil {
		return err
	}
	worker := maintenance.Worker{Store: store, Runner: maintenance.ExecRunner{}, Holds: collectors.NewDetachedAptHolds()}
	prior, err := store.Current()
	if err == nil && (!prior.Terminal() || prior.HoldsReleased) {
		if archived, archiveErr := maintenance.ReadTerminal(prior.CommandID); archiveErr == nil && archived.Terminal() && !archived.HoldsReleased {
			if err = store.Save(archived); err != nil {
				return err
			}
			return errors.New("previous durable terminal result restored; fresh operator retry required")
		}
		_, recoveryErr := worker.RecoverInterrupted(ctx, prior)
		return errors.Join(errors.New("previous maintenance interrupted; holds recovery attempted; fresh operator retry required"), recoveryErr)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read previous status: %w", err)
	}
	req, err := maintenance.ReadInbox()
	if err != nil {
		return err
	}
	_, err = worker.Execute(ctx, req)
	return err
}
