package updater

import (
	"context"
	"log"
	"time"

	"github.com/billypuding/watplan/internal/wat"
)

const directoryTTL = time.Hour

func (u *Updater) directory() *wat.Directory {
	u.dirMu.Lock()
	defer u.dirMu.Unlock()
	if u.dir != nil && time.Since(u.dirAt) < directoryTTL {
		return u.dir
	}
	var names []string
	for _, id := range u.store.PlanIDs() {
		plan, err := u.store.Load(id)
		if err != nil {
			continue
		}
		names = append(names, wat.TeacherNames(plan.Events)...)
	}
	u.dir = wat.NewDirectory(names)
	u.dirAt = time.Now()
	return u.dir
}

func (u *Updater) ResolveStoredTeachers(ctx context.Context) {
	dir := u.directory()
	plans, events := 0, 0
	for _, id := range u.store.PlanIDs() {
		if ctx.Err() != nil {
			return
		}
		lock := u.planLock(id)
		lock.Lock()
		plan, err := u.store.Load(id)
		if err == nil {
			var n int
			plan.Events, n = wat.ResolveTeachers(plan.Events, dir)
			if n > 0 {
				if err := u.store.Save(id, plan); err != nil {
					log.Printf("plan %s: zapis rozwiniętych skrótów: %v", id, err)
				} else {
					plans++
					events += n
				}
			}
		}
		lock.Unlock()
	}
	log.Printf("skróty wykładowców: rozwinięto %d zajęć w %d planach", events, plans)
}
