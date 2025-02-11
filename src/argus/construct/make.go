// Copyright (c) 2017
// Author: Jeff Weisberg <jaw @ tcp4me.com>
// Created: 2017-Sep-05 19:45 (EDT)
// Function: make things

package construct

import (
	"argus.domain/argus/configure"
	//"github.com/jaw0/acgo/diag"
	"argus.domain/argus/alias"
	"argus.domain/argus/darp"
	"argus.domain/argus/group"
	"argus.domain/argus/monel"
	"argus.domain/argus/monitor/agent"
	"argus.domain/argus/monitor/snmp"
	"argus.domain/argus/notify"
	"argus.domain/argus/service"
)

func Make(cf *configure.CF, parent *monel.M) *monel.M {

	dl.Debug("make %s; %s", cf.Type, cf.Name)

	switch cf.Type {
	case "service":
		s, err := service.New(cf, parent)
		if err != nil {
			cf.Error("%v", err)
		}
		return s
	case "host":
		_, exist := cf.Param["hostname"]
		if !exist {
			cf.Param["hostname"] = &configure.CFV{Value: cf.Name, Line: cf.Line}
		}
		fallthrough
	case "group":
		g, err := group.New(cf, parent)
		if err != nil {
			cf.Error("%v", err)
		}
		return g
	case "alias":
		a, err := alias.New(cf, parent)
		if err != nil {
			cf.Error("%v", err)
		}
		return a
	case "method":
		err := notify.NewMethod(cf)
		if err != nil {
			cf.Error("%v", err)
		}

	case "darp":
		err := darp.New(cf)
		if err != nil {
			cf.Error("%v", err)
		}

	case "snmpoid":
		err := snmp.NewOID(cf)
		if err != nil {
			cf.Error("%v", err)
		}
	case "agent":
		err := agent.NewAgent(cf)
		if err != nil {
			cf.Error("%v", err)
		}
	default:
		dl.Bug("unable to construct object of type '%s'", cf.Type)
	}
	return nil
}
