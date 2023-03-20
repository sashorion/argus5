// Copyright (c) 2017
// Author: Jeff Weisberg <jaw @ tcp4me.com>
// Created: 2017-Sep-06 19:19 (EDT)
// Function: status transition

package monel

import (
	"argus.domain/argus/argus"
	"argus.domain/argus/clock"
	"argus.domain/argus/notify"
)

// m is a service updating status
func (m *M) Update(status argus.Status, result string, reason string) {

	m.Debug("mon/update %s -> %s", m.P.Status, status)
	m.Lock.Lock()
	defer m.Lock.Unlock()
	prev := m.P.OvStatus
	changed := m.updateStatus(status, result, reason)

	if !changed {
		return
	}

	m.commonUpdate(prev)
}

func (m *M) updateStatus(status argus.Status, result string, reason string) bool {

	m.P.Result = result
	if status == m.P.Status {
		return false
	}
	m.P.Status = status
	m.P.Reason = reason

	return m.determineStatus()
}

// update group status + ovstatus
func (m *M) ReUpdate(reason string) {

	m.Lock.Lock()
	defer m.Lock.Unlock()
	prev := m.P.OvStatus
	changed := m.determineStatus()

	if !changed {
		m.commonUpdateNoChange()
		return
	}
	if reason != "" {
		m.P.Reason = reason
	}

	m.commonUpdate(prev)
}

// update group status + ovstatus
func (m *M) UpUpdate(by *M) {
	m.ReUpdate(by.Cf.Uname)
}

func (m *M) commonUpdate(prevOv argus.Status) {

	m.WebTime = clock.Nano()
	m.setAlarm()
	go m.updateIsDown(m.P.OvStatus) // needs a different lock
	m.loggitL("TRANSITION", m.P.Reason)
	dl.Verbose("TRANSITION [%s -> %s] %s (%s)", prevOv, m.P.OvStatus, m.Cf.Unique, m.P.Reason)
	m.statsTransition()
	m.determineSummary()
	m.updateNotifies()
	m.maybeNotify(prevOv)

	// RSN - audit hook
	// RSN - if up + ov + auto -> remove

	m.andUpwards()
}

// no change in status, but we still need to update the count summaries
func (m *M) commonUpdateNoChange() {

	m.WebTime = clock.Nano()
	m.statsTransition()
	m.determineSummary()
	m.setAlarm()
	go m.updateIsDown(m.P.OvStatus)
	m.andUpwards()
}

func (m *M) updateIsDown(ovstatus argus.Status) {

	lock.Lock()
	defer lock.Unlock()

	// keep track of all down elements - for reporting
	switch ovstatus {
	case argus.UNKNOWN, argus.CLEAR, argus.OVERRIDE, argus.DEPENDS:
		delete(isdown, m.Cf.Unique)
	default:
		isdown[m.Cf.Unique] = m
	}

}

func (m *M) andUpwards() {

	var upup []*M
	notfirst := false
	// propagate upwards!
	for _, parent := range m.Parent {
		if notfirst && len(parent.Children) == 1 {
			// update alias
			upup = append(upup, parent.Children[0])
			continue
		}
		upup = append(upup, parent)
		notfirst = true
	}

	go func(){
		for _, u := range upup {
			dl.Debug("and upwards %s -> %s", m.Cf.Unique, u.Cf.Unique)
			u.UpUpdate(m)
		}
	}()


	// and anything depending on me?
}

// ################################################################

func (m *M) maybeNotify(prevOv argus.Status) {

	if m.NotifyCf == nil {
		return
	}

	st := m.P.OvStatus
	if prevOv == argus.UNKNOWN && st == argus.CLEAR {
		return
	}
	if st == argus.OVERRIDE || st == argus.DEPENDS || st == argus.UNKNOWN {
		return
	}
	if st == argus.CLEAR && (prevOv == argus.OVERRIDE || prevOv == argus.DEPENDS) {
		// do not notify if we went from override/depends -> up
		return
	}
	if m.P.AncInOv {
		// do not notify if an ancestor is in override
		return
	}

	if !m.permitNotify(st) {
		return
	}

	// ...

	m.Debug("send notify")

	notif := notify.New(&notify.NewConf{
		Unique:       m.Cf.Unique,
		FriendlyName: m.Cf.Friendlyname,
		ShortName:    m.Cf.Label,
		Conf:         m.NotifyCf,
		Reason:       m.P.Reason,
		Result:       m.P.Result,
		OvStatus:     st,
		PrevOv:       prevOv,
	}, m)

	if notif != nil {
		m.Notifies = append(m.Notifies, notif)
	}

}

// tell existing notifications that the status changed
func (m *M) updateNotifies() {

	for _, n := range m.Notifies {
		m.Debug("update notify")
		n.Update(m.P.OvStatus)
	}
}

func (m *M) permitNotify(status argus.Status) bool {

	if int(status) >= len(m.Cf.Sendnotify) {
		return false
	}

	if m.Cf.Sendnotify[int(status)] != nil {
		return m.Cf.Sendnotify[int(status)].PermitNow("yes")
	}
	if m.Cf.Sendnotify[int(argus.UNKNOWN)] != nil {
		return m.Cf.Sendnotify[int(argus.UNKNOWN)].PermitNow("yes")
	}
	return false
}

func (m *M) setAlarm() {

	m.P.TransTime = clock.Nano()
	a := false

	if m.P.OvStatus > argus.CLEAR && m.P.OvStatus <= argus.CRITICAL {
		if m.Cf.Siren[int(m.P.OvStatus)] || m.Cf.Siren[int(argus.UNKNOWN)] {
			a = true
		}
	}

	if a != m.P.Alarm {
		m.P.Alarm = a
		m.P.SirenTime = m.P.TransTime
	}
}

// ################################################################

// determine status + ovstatus
// lock should already be held
func (m *M) determineStatus() bool {

	prevo := m.P.OvStatus

	m.determineAggrStatus()
	m.checkDepends()
	m.checkOverride()

	dl.Debug("%s : %s -> %s", m.Cf.Unique, prevo, m.P.OvStatus)
	return m.P.OvStatus != prevo
}

func (m *M) checkOverride() {

	if m.P.Status == argus.CLEAR || m.P.Status == argus.UNKNOWN {
		return
	}
	if m.P.OvStatus == argus.DEPENDS {
		return
	}

	if m.P.Override != nil {
		m.P.OvStatus = argus.OVERRIDE
	}
}

// dtermine our aggregate status
// lock should already be held
func (m *M) determineAggrStatus() {

	children := m.Me.Children()
	dl.Debug("%s nchild %d", m.Cf.Unique, len(children))
	if len(children) == 0 {
		m.P.OvStatus = m.P.Status
		return
	}

	nchild := 0
	rsum := [argus.MAXSTATUS + 1]int{}
	osum := [argus.MAXSTATUS + 1]int{}

	for _, child := range children {
		rs, os := child.Status()
		dl.Debug("%s %s : rs %v, os %v", m.Cf.Unique, child.Cf.Unique, rs, os)
		rsum[rs]++
		osum[os]++
		nchild++
	}

	rs := calcAggrStatus(m.Cf.Gravity, nchild, argus.CRITICAL, rsum[:])
	os := calcAggrStatus(m.Cf.Gravity, nchild, argus.MAXSTATUS, osum[:])

	dl.Debug("%s rs %v %v, os %v %v", m.Cf.Unique, rsum, rs, osum, os)
	m.P.Status = rs
	m.P.OvStatus = os
}

func calcAggrStatus(grav argus.Gravity, tot int, max argus.Status, statuses []int) argus.Status {

	tot -= statuses[int(argus.UNKNOWN)]

	switch grav {
	case argus.GRAV_DN:
		if statuses[int(argus.CRITICAL)] > 0 {
			return argus.CRITICAL
		}
		for sev := max; sev >= argus.CLEAR; sev-- {
			if statuses[int(sev)] > 0 {
				return sev
			}
		}

	case argus.GRAV_UP:
		for sev := argus.CLEAR; sev <= max; sev++ {
			if statuses[int(sev)] > 0 {
				return sev
			}
		}
		return argus.CLEAR
	default:
		lim := (tot + 1) / 2
		cum := 0
		for sev := argus.CLEAR; sev <= max; sev++ {
			cum += statuses[int(sev)]
			if cum >= lim {
				return sev
			}
		}
	}

	return argus.UNKNOWN
}

func (m *M) determineSummary() {

	for i := 0; i <= int(argus.MAXSTATUS); i++ {
		// reset to 0
		m.P.OvStatusSummary[i] = 0
	}

	children := m.Me.Children()

	if len(children) == 0 || m.Cf.Countstop {
		m.P.OvStatusSummary[int(m.P.OvStatus)] = 1
		return
	}

	// QQQ - do this on the web side, instead?
	if m.P.Override != nil {
		m.P.OvStatusSummary[int(argus.OVERRIDE)] = len(m.Children)
		return
	}

	for _, child := range children {

		child.Lock.RLock()
		for i := 0; i <= int(argus.MAXSTATUS); i++ {
			if m.P.Override != nil && i >= int(argus.WARNING) && i <= int(argus.CRITICAL) {
				m.P.OvStatusSummary[int(argus.OVERRIDE)] += child.P.OvStatusSummary[i]
			} else {
				m.P.OvStatusSummary[i] += child.P.OvStatusSummary[i]
			}
		}
		child.Lock.RUnlock()
	}

	dl.Debug("summy: %s %v", m.Cf.Unique, m.P.OvStatusSummary)
}
