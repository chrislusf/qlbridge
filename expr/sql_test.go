package expr

import (
	"fmt"
	"reflect"
	"testing"

	u "github.com/araddon/gou"
	"github.com/bmizerany/assert"
)

var (
	_ = fmt.Sprint
	_ = u.EMPTY

	sqlStrings = []string{`/*
	DESCRIPTION
*/
SELECT 
    fname
    , lname AS last_name
    , count(host(_ses)) IF contains(_ses,"google.com")
    , now() AS created_ts
    , name   -- comment 
    , valuect(event) 
    , todate(reg_date)
    , todate(` + "`field xyz $%`" + `)
INTO table 
FROM mystream 
WHERE 
   ne(event,"stuff") AND ge(party, 1)
`, `/*
	  multi line comment
*/
	SELECT 
	    fname -- First Name
	    , lname AS last_name 
	    , count(_ses) IF contains(_ses,google.com)
	    , email
	    , set(cc)          AS choices 
	FROM mystream 
	WHERE 
	   ne(event,"stuff") AND ge(party, 1)
`,
		`SELECT 
			u.user_id, u.email, o.item_id,o.price
		FROM users AS u 
		INNER JOIN orders AS o 
		ON u.user_id = o.user_id;
	`}
)

func parseOrPanic(t *testing.T, query string) SqlStatement {
	stmt, err := ParseSql(query)
	if err != nil {
		t.Errorf("Parse failed: %s \n%s", query, err)
		t.FailNow()
	}
	return stmt
}

// We need to be able to re-write queries, as we during joins we have
// to re-write query that we are going to send to a single data source
func TestToSql(t *testing.T) {
	for _, sqlStrIn := range sqlStrings {
		u.Debug("parsing next one ", sqlStrIn)
		stmt1 := parseOrPanic(t, sqlStrIn)
		sqlSel1 := stmt1.(*SqlSelect)
		sqlRt := sqlSel1.StringAST()
		u.Warnf("About to parse roundtrip \n%v", sqlRt)
		stmt2 := parseOrPanic(t, sqlRt)
		compareAst(t, stmt1, stmt2)
	}
}

func compareFroms(t *testing.T, fl, fr []*SqlSource) {
	assert.T(t, len(fl) == len(fr), "must have same froms")
	for i, f := range fl {
		compareFrom(t, f, fr[i])
	}
}

func compareFrom(t *testing.T, fl, fr *SqlSource) {
	assert.T(t, fl.Name == fr.Name)
	assert.Equal(t, fl.Op, fr.Op)
	assert.Equal(t, fl.Alias, fr.Alias)
	assert.Equal(t, fl.LeftOrRight, fr.LeftOrRight)
	assert.Equal(t, fl.JoinType, fr.JoinType)
	compareNode(t, fl.JoinExpr, fr.JoinExpr)
}

func compareAstColumn(t *testing.T, colLeft, colRight *Column) {
	//assert.Tf(t, colLeft.LongDesc == colRight.LongDesc, "Longdesc")

	assert.Tf(t, colLeft.As == colRight.As, "As: '%v' != '%v'", colLeft.As, colRight.As)

	assert.Tf(t, colLeft.Comment == colRight.Comment, "Comments?  '%s' '%s'", colLeft.Comment, colRight.Comment)

	compareNode(t, colLeft.Guard, colRight.Guard)
	compareNode(t, colLeft.Expr, colRight.Expr)

}

func compareAst(t *testing.T, in1, in2 SqlStatement) {

	switch s1 := in1.(type) {
	case *SqlSelect:
		s2, ok := in2.(*SqlSelect)
		assert.T(t, ok, "Must also be SqlSelect")
		u.Debugf("original:\n%s", s1.StringAST())
		u.Debugf("after:\n%s", s2.StringAST())
		//assert.T(t, s1.Alias == s2.Alias)
		//assert.T(t, len(s1.Columns) == len(s2.Columns))
		for i, c := range s1.Columns {
			compareAstColumn(t, c, s2.Columns[i])
		}
		//compareWhere(s1.Where)
		compareFroms(t, s1.From, s2.From)
	default:
		t.Fatalf("Must be SqlSelect")
	}
}

func compareNode(t *testing.T, n1, n2 Node) {
	if n1 == nil && n2 == nil {
		return
	}
	rv1, rv2 := reflect.ValueOf(n1), reflect.ValueOf(n2)
	assert.Tf(t, rv1.Kind() == rv2.Kind(), "kinds match: %T %T", n1, n2)
}

func TestSqlRewrite(t *testing.T) {
	s := `SELECT u.name, u.email, o.item_id, o.price
			FROM users AS u INNER JOIN orders AS o 
			ON u.user_id = o.user_id;`
	/*
		- Do we want to send the columns fully aliased?   ie
			SELECT name AS u.name, email as u.email, user_id as u.user_id FROM users

	*/
	sql := parseOrPanic(t, s).(*SqlSelect)
	assert.Tf(t, len(sql.Columns) == 4, "has 4 cols: %v", len(sql.Columns))
	assert.Tf(t, len(sql.From) == 2, "has 2 sources: %v", len(sql.From))

	// Test the Left/Right column level parsing
	//  TODO:   This field should not be u.name?   sourcefield should be name right? as = u.name?
	col, _ := sql.Columns.ByName("u.name")
	assert.Tf(t, col.As == "u.name", "col.As=%s", col.As)
	left, right, ok := col.LeftRight()
	//u.Debugf("left=%v  right=%v  ok%v", left, right, ok)
	assert.T(t, left == "u" && right == "name" && ok == true)

	rw1 := sql.From[0].Rewrite(true, sql)
	assert.Tf(t, rw1 != nil, "should not be nil:")
	assert.Tf(t, len(rw1.Columns) == 3, "has 3 cols: %v", rw1.Columns.String())
	//u.Infof("SQL?: '%v'", rw1.String())
	assert.Tf(t, rw1.String() == "SELECT name, email, user_id FROM users", "%v", rw1.String())

	rw1 = sql.From[1].Rewrite(false, sql)
	assert.Tf(t, rw1 != nil, "should not be nil:")
	assert.Tf(t, len(rw1.Columns) == 3, "has 3 cols: %v", rw1.Columns.String())
	//u.Infof("SQL?: '%v'", rw1.String())
	assert.Tf(t, rw1.String() == "SELECT item_id, price, user_id FROM orders", "%v", rw1.String())

	// Do we change?
	//assert.Equal(t, sql.Columns.FieldNames(), []string{"user_id", "email", "item_id", "price"})

	s = `SELECT u.name, u.email, b.title
			FROM users AS u INNER JOIN blog AS b 
			ON u.name = b.author;`
	sql = parseOrPanic(t, s).(*SqlSelect)
	assert.Tf(t, len(sql.Columns) == 3, "has 3 cols: %v", len(sql.Columns))
	assert.Tf(t, len(sql.From) == 2, "has 2 sources: %v", len(sql.From))
	rw1 = sql.From[0].Rewrite(true, sql)
	assert.Tf(t, rw1 != nil, "should not be nil:")
	assert.Tf(t, len(rw1.Columns) == 2, "has 2 cols: %v", rw1.Columns.String())
	//u.Infof("SQL?: '%v'", rw1.String())
	assert.Tf(t, rw1.String() == "SELECT name, email FROM users", "%v", rw1.String())
	jn, _ := sql.From[0].JoinValueExpr()
	assert.Tf(t, jn.String() == "name", "%v", jn.String())
	cols := sql.From[0].UnAliasedColumns()
	assert.Tf(t, len(cols) == 2, "Should have 2: %#v", cols)

	u.Infof("cols: %#v", cols)

	rw1 = sql.From[1].Rewrite(false, sql)
	assert.Tf(t, rw1 != nil, "should not be nil:")
	assert.Tf(t, len(rw1.Columns) == 2, "has 2 cols: %v", rw1.Columns.String())
	// TODO:   verify that we can rewrite sql for aliases
	// jn, _ = sql.From[1].JoinValueExpr()
	// assert.Tf(t, jn.String() == "name", "%v", jn.String())
	// u.Infof("SQL?: '%v'", rw1.String())
	// assert.Tf(t, rw1.String() == "SELECT title, author as name FROM blog", "%v", rw1.String())

	// This test, is looking at these aspects of rewrite
	//  1 the dotted notation of 'repostory.name' ensuring we have removed the p.
	//  2 where clause
	s = `
		SELECT 
			p.actor, p.repository.name, a.title
		FROM article AS a 
		INNER JOIN github_push AS p 
			ON p.actor = a.author
		WHERE p.follow_ct > 20 AND a.email IS NOT NULL
	`
	sql = parseOrPanic(t, s).(*SqlSelect)
	assert.Tf(t, len(sql.Columns) == 3, "has 3 cols: %v", len(sql.Columns))
	assert.Tf(t, len(sql.From) == 2, "has 2 sources: %v", len(sql.From))

	rw0 := sql.From[0].Rewrite(false, sql)
	rw1 = sql.From[1].Rewrite(false, sql)
	assert.Tf(t, rw0 != nil, "should not be nil:")
	assert.Tf(t, len(rw0.Columns) == 2, "has 2 cols: %v", rw0.String())
	assert.Tf(t, rw0.String() == "SELECT title, author FROM article WHERE email != NULL", "Wrong SQL 0: %v", rw0.String())
	assert.Tf(t, rw1 != nil, "should not be nil:")
	assert.Tf(t, len(rw1.Columns) == 2, "has 2 cols: %v", rw1.Columns.String())
	assert.Tf(t, rw1.String() == "SELECT actor, repository.name FROM github_push WHERE follow_ct > 20", "Wrong SQL 1: %v", rw1.String())

	// Original should still be the same
	assert.Tf(t, sql.String() == "SELECT p.actor, p.repository.name, a.title FROM article AS a INNER JOIN github_push AS p ON p.actor = a.author WHERE p.follow_ct > 20 AND a.email != NULL", "Wrong Full SQL?: '%v'", sql.String())
}
