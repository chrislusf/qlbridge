package exec

import (
	u "github.com/araddon/gou"
	"github.com/araddon/qlbridge/datasource"
	"github.com/araddon/qlbridge/expr"
	"github.com/araddon/qlbridge/value"
	"github.com/araddon/qlbridge/vm"
)

// A scanner to filter by where clause
type Where struct {
	*TaskBase
	where expr.Node
}

func NewWhere(where expr.Node, stmt *expr.SqlSelect) *Where {
	s := &Where{
		TaskBase: NewTaskBase("Where"),
		where:    where,
	}
	cols := make(map[string]*expr.Column)
	if len(stmt.From) == 1 {
		cols = stmt.UnAliasedColumns()
	} else {
		for _, from := range stmt.From {
			//u.Debugf("cols: %v", from.Columns)
			for _, col := range from.Columns {
				_, right, _ := col.LeftRight()
				if _, ok := cols[right]; !ok {
					//u.Debugf("col: %#v", col)
					cols[right] = col.Copy()
					cols[right].Index = len(cols) - 1
				} else {
					//u.Debugf("has col: %#v", col)
				}
			}
		}
	}

	//u.Debugf("found where columns: %d", len(cols))

	s.Handler = whereFilter(where, s, cols)
	return s
}

func whereFilter(where expr.Node, task TaskRunner, cols map[string]*expr.Column) MessageHandler {
	out := task.MessageOut()
	evaluator := vm.Evaluator(where)
	return func(ctx *Context, msg datasource.Message) bool {
		// defer func() {
		// 	if r := recover(); r != nil {
		// 		u.Errorf("crap, %v", r)
		// 	}
		// }()
		var whereValue value.Value
		var ok bool

		switch mt := msg.(type) {
		case *datasource.SqlDriverMessage:
			//u.Debugf("WHERE:  T:%T  vals:%#v", msg, mt.Vals)
			//u.Debugf("cols:  %#v", cols)
			msgReader := datasource.NewValueContextWrapper(mt, cols)
			whereValue, ok = evaluator(msgReader)
		case *datasource.SqlDriverMessageMap:
			whereValue, ok = evaluator(mt)
		default:
			if msgReader, ok := msg.(expr.ContextReader); ok {
				whereValue, ok = evaluator(msgReader)
			} else {
				u.Errorf("could not convert to message reader: %T", msg)
			}
		}
		//u.Debugf("msg: %#v", msgReader)
		//u.Infof("evaluating: ok?%v  result=%v where expr:%v", ok, whereValue.ToString(), where.StringAST())
		if !ok {
			u.Errorf("could not evaluate: %v", msg)
			return false
		}
		switch whereVal := whereValue.(type) {
		case value.BoolValue:
			if whereVal.Val() == false {
				u.Debugf("Filtering out: T:%T   v:%#v", whereVal, whereVal)
				return true
			}
		default:
			u.Warnf("unknown type? %T", whereVal)
		}
		//u.Debug("about to send from where to forward: %#v", msg)
		select {
		case out <- msg:
			return true
		case <-task.SigChan():
			return false
		}
	}
}
