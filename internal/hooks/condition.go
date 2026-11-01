package hooks

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Hook conditions are JavaScript expressions (the config file already grants
// arbitrary shell execution through command actions, so evaluating expressions
// from the same source adds no new privilege). This evaluator implements the
// subset used by conditions: literals, identifiers, member access, method
// calls, regex literals with .test, comparisons, and boolean logic.

type condValue any

type condTokenType int

const (
	tokEOF condTokenType = iota
	tokIdent
	tokString
	tokNumber
	tokRegex
	tokPunct
)

type condToken struct {
	typ condTokenType
	str string
	num float64
	re  *regexp.Regexp
}

func isIdentStart(r byte) bool {
	return r == '_' || r == '$' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func isIdentPart(r byte) bool {
	return isIdentStart(r) || (r >= '0' && r <= '9')
}

func tokenizeCondition(src string) ([]condToken, error) {
	var tokens []condToken
	i := 0
	valueEnd := func() bool {
		if len(tokens) == 0 {
			return false
		}
		last := tokens[len(tokens)-1]
		switch last.typ {
		case tokIdent, tokString, tokNumber, tokRegex:
			return true
		case tokPunct:
			return last.str == ")" || last.str == "]"
		}
		return false
	}
	for i < len(src) {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case isIdentStart(c):
			start := i
			for i < len(src) && isIdentPart(src[i]) {
				i++
			}
			tokens = append(tokens, condToken{typ: tokIdent, str: src[start:i]})
		case c == '"' || c == '\'':
			quote := c
			i++
			var sb strings.Builder
			for i < len(src) && src[i] != quote {
				if src[i] == '\\' && i+1 < len(src) {
					i++
					switch src[i] {
					case 'n':
						sb.WriteByte('\n')
					case 't':
						sb.WriteByte('\t')
					case 'r':
						sb.WriteByte('\r')
					default:
						sb.WriteByte(src[i])
					}
					i++
					continue
				}
				sb.WriteByte(src[i])
				i++
			}
			if i >= len(src) {
				return nil, fmt.Errorf("unterminated string literal")
			}
			i++
			tokens = append(tokens, condToken{typ: tokString, str: sb.String()})
		case c >= '0' && c <= '9':
			start := i
			for i < len(src) && (src[i] >= '0' && src[i] <= '9' || src[i] == '.' || src[i] == 'e' || src[i] == 'E' || src[i] == '+' || src[i] == '-') {
				if (src[i] == '+' || src[i] == '-') && i > start && src[i-1] != 'e' && src[i-1] != 'E' {
					break
				}
				i++
			}
			n, err := strconv.ParseFloat(src[start:i], 64)
			if err != nil {
				return nil, fmt.Errorf("invalid number literal %q", src[start:i])
			}
			tokens = append(tokens, condToken{typ: tokNumber, num: n})
		case c == '/' && !valueEnd():
			i++
			var pattern strings.Builder
			inClass := false
			for i < len(src) {
				ch := src[i]
				if ch == '\\' && i+1 < len(src) {
					pattern.WriteByte(ch)
					pattern.WriteByte(src[i+1])
					i += 2
					continue
				}
				if ch == '[' {
					inClass = true
				} else if ch == ']' {
					inClass = false
				} else if ch == '/' && !inClass {
					break
				}
				pattern.WriteByte(ch)
				i++
			}
			if i >= len(src) {
				return nil, fmt.Errorf("unterminated regex literal")
			}
			i++
			for i < len(src) && isIdentPart(src[i]) {
				i++
			}
			re, err := regexp.Compile(pattern.String())
			if err != nil {
				return nil, fmt.Errorf("invalid regex: %w", err)
			}
			tokens = append(tokens, condToken{typ: tokRegex, re: re})
		default:
			matched := false
			for _, op := range []string{"===", "!==", "==", "!=", "&&", "||", "<=", ">="} {
				if strings.HasPrefix(src[i:], op) {
					tokens = append(tokens, condToken{typ: tokPunct, str: op})
					i += len(op)
					matched = true
					break
				}
			}
			if !matched {
				if strings.ContainsRune("()!.<>,", rune(c)) {
					tokens = append(tokens, condToken{typ: tokPunct, str: string(c)})
					i++
				} else {
					return nil, fmt.Errorf("unexpected character %q", c)
				}
			}
		}
	}
	tokens = append(tokens, condToken{typ: tokEOF})
	return tokens, nil
}

type condNode interface{}

type literalNode struct{ value condValue }
type identNode struct{ name string }
type memberNode struct {
	object condNode
	prop   string
}
type callNode struct {
	callee condNode
	args   []condNode
}
type unaryNode struct {
	op      string
	operand condNode
}
type binaryNode struct {
	op    string
	left  condNode
	right condNode
}

type condParser struct {
	tokens []condToken
	pos    int
}

func (p *condParser) peek() condToken { return p.tokens[p.pos] }
func (p *condParser) next() condToken { t := p.tokens[p.pos]; p.pos++; return t }

func (p *condParser) acceptPunct(op string) bool {
	if t := p.peek(); t.typ == tokPunct && t.str == op {
		p.pos++
		return true
	}
	return false
}

func (p *condParser) parseExpression() (condNode, error) { return p.parseOr() }

func (p *condParser) parseOr() (condNode, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.acceptPunct("||") {
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = binaryNode{op: "||", left: left, right: right}
	}
	return left, nil
}

func (p *condParser) parseAnd() (condNode, error) {
	left, err := p.parseEquality()
	if err != nil {
		return nil, err
	}
	for p.acceptPunct("&&") {
		right, err := p.parseEquality()
		if err != nil {
			return nil, err
		}
		left = binaryNode{op: "&&", left: left, right: right}
	}
	return left, nil
}

func (p *condParser) parseEquality() (condNode, error) {
	left, err := p.parseRelational()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if t.typ != tokPunct || (t.str != "===" && t.str != "!==" && t.str != "==" && t.str != "!=") {
			return left, nil
		}
		p.pos++
		right, err := p.parseRelational()
		if err != nil {
			return nil, err
		}
		left = binaryNode{op: t.str, left: left, right: right}
	}
}

func (p *condParser) parseRelational() (condNode, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if t.typ != tokPunct || (t.str != "<" && t.str != ">" && t.str != "<=" && t.str != ">=") {
			return left, nil
		}
		p.pos++
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = binaryNode{op: t.str, left: left, right: right}
	}
}

func (p *condParser) parseUnary() (condNode, error) {
	if p.acceptPunct("!") {
		operand, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return unaryNode{op: "!", operand: operand}, nil
	}
	return p.parsePostfix()
}

func (p *condParser) parsePostfix() (condNode, error) {
	node, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	for p.acceptPunct(".") {
		t := p.next()
		if t.typ != tokIdent {
			return nil, fmt.Errorf("expected property name after '.'")
		}
		node = memberNode{object: node, prop: t.str}
		if p.acceptPunct("(") {
			var args []condNode
			for !p.acceptPunct(")") {
				if len(args) > 0 {
					if !p.acceptPunct(",") {
						return nil, fmt.Errorf("expected ',' in argument list")
					}
				}
				arg, err := p.parseExpression()
				if err != nil {
					return nil, err
				}
				args = append(args, arg)
			}
			node = callNode{callee: node, args: args}
		}
	}
	return node, nil
}

func (p *condParser) parsePrimary() (condNode, error) {
	t := p.next()
	switch t.typ {
	case tokString:
		return literalNode{value: t.str}, nil
	case tokNumber:
		return literalNode{value: t.num}, nil
	case tokRegex:
		return literalNode{value: t.re}, nil
	case tokIdent:
		switch t.str {
		case "true":
			return literalNode{value: true}, nil
		case "false":
			return literalNode{value: false}, nil
		case "null", "undefined":
			return literalNode{value: nil}, nil
		}
		return identNode{name: t.str}, nil
	case tokPunct:
		if t.str == "(" {
			node, err := p.parseExpression()
			if err != nil {
				return nil, err
			}
			if !p.acceptPunct(")") {
				return nil, fmt.Errorf("expected ')'")
			}
			return node, nil
		}
	}
	return nil, fmt.Errorf("unexpected token")
}

func condTruthiness(v condValue) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case float64:
		return t != 0 && !math.IsNaN(t)
	case string:
		return t != ""
	default:
		return true
	}
}

func condToNumber(v condValue) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case string:
		if strings.TrimSpace(t) == "" {
			return 0, true
		}
		n, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return n, err == nil
	case bool:
		if t {
			return 1, true
		}
		return 0, true
	case nil:
		return 0, true
	}
	return 0, false
}

func condStrictEquals(a, b condValue) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	switch x := a.(type) {
	case string:
		y, ok := b.(string)
		return ok && x == y
	case float64:
		y, ok := b.(float64)
		return ok && x == y
	case bool:
		y, ok := b.(bool)
		return ok && x == y
	default:
		return a == b
	}
}

func condLooseEquals(a, b condValue) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if af, ok := condToNumber(a); ok {
		if bf, ok := condToNumber(b); ok {
			return af == bf
		}
	}
	return condStrictEquals(a, b)
}

func condCompare(a, b condValue) (int, bool) {
	if af, ok := condToNumber(a); ok {
		if bf, ok := condToNumber(b); ok {
			switch {
			case af < bf:
				return -1, true
			case af > bf:
				return 1, true
			default:
				return 0, true
			}
		}
	}
	if as, ok := a.(string); ok {
		if bs, ok := b.(string); ok {
			return strings.Compare(as, bs), true
		}
	}
	return 0, false
}

func evalCondNode(node condNode, env map[string]condValue) (condValue, error) {
	switch n := node.(type) {
	case literalNode:
		return n.value, nil
	case identNode:
		return env[n.name], nil
	case memberNode:
		object, err := evalCondNode(n.object, env)
		if err != nil {
			return nil, err
		}
		switch obj := object.(type) {
		case map[string]any:
			return obj[n.prop], nil
		case string:
			if n.prop == "length" {
				return float64(len(obj)), nil
			}
		}
		return nil, nil
	case callNode:
		member, ok := n.callee.(memberNode)
		if !ok {
			return nil, fmt.Errorf("unsupported call")
		}
		object, err := evalCondNode(member.object, env)
		if err != nil {
			return nil, err
		}
		args := make([]condValue, len(n.args))
		for i, arg := range n.args {
			if args[i], err = evalCondNode(arg, env); err != nil {
				return nil, err
			}
		}
		return callCondMethod(object, member.prop, args)
	case unaryNode:
		operand, err := evalCondNode(n.operand, env)
		if err != nil {
			return nil, err
		}
		return !condTruthiness(operand), nil
	case binaryNode:
		left, err := evalCondNode(n.left, env)
		if err != nil {
			return nil, err
		}
		switch n.op {
		case "&&":
			if !condTruthiness(left) {
				return left, nil
			}
			return evalCondNode(n.right, env)
		case "||":
			if condTruthiness(left) {
				return left, nil
			}
			return evalCondNode(n.right, env)
		}
		right, err := evalCondNode(n.right, env)
		if err != nil {
			return nil, err
		}
		switch n.op {
		case "===":
			return condStrictEquals(left, right), nil
		case "!==":
			return !condStrictEquals(left, right), nil
		case "==":
			return condLooseEquals(left, right), nil
		case "!=":
			return !condLooseEquals(left, right), nil
		}
		cmp, ok := condCompare(left, right)
		if !ok {
			return false, nil
		}
		switch n.op {
		case "<":
			return cmp < 0, nil
		case ">":
			return cmp > 0, nil
		case "<=":
			return cmp <= 0, nil
		case ">=":
			return cmp >= 0, nil
		}
		return nil, fmt.Errorf("unsupported operator %s", n.op)
	}
	return nil, fmt.Errorf("unsupported expression node")
}

func callCondMethod(object condValue, method string, args []condValue) (condValue, error) {
	strArg := func() (string, error) {
		if len(args) != 1 {
			return "", fmt.Errorf("%s expects one argument", method)
		}
		s, ok := args[0].(string)
		if !ok {
			return "", fmt.Errorf("%s expects a string argument", method)
		}
		return s, nil
	}
	switch obj := object.(type) {
	case *regexp.Regexp:
		if method == "test" {
			s, err := strArg()
			if err != nil {
				return nil, err
			}
			return obj.MatchString(s), nil
		}
	case string:
		switch method {
		case "startsWith", "endsWith", "includes":
			s, err := strArg()
			if err != nil {
				return nil, err
			}
			switch method {
			case "startsWith":
				return strings.HasPrefix(obj, s), nil
			case "endsWith":
				return strings.HasSuffix(obj, s), nil
			default:
				return strings.Contains(obj, s), nil
			}
		case "match":
			if len(args) != 1 {
				return nil, fmt.Errorf("match expects one argument")
			}
			re, ok := args[0].(*regexp.Regexp)
			if !ok {
				return nil, fmt.Errorf("match expects a regex argument")
			}
			if re.MatchString(obj) {
				return true, nil
			}
			return nil, nil
		case "toLowerCase":
			return strings.ToLower(obj), nil
		case "toUpperCase":
			return strings.ToUpper(obj), nil
		}
	}
	return nil, fmt.Errorf("unsupported method %s", method)
}

// evaluateCondition evaluates a JavaScript-subset condition against the hook
// context. Broken conditions are skipped silently (the caller logs them).
func evaluateCondition(condition string, ctx HookContext) (bool, error) {
	tokens, err := tokenizeCondition(condition)
	if err != nil {
		return false, err
	}
	p := &condParser{tokens: tokens}
	node, err := p.parseExpression()
	if err != nil {
		return false, err
	}
	if p.peek().typ != tokEOF {
		return false, fmt.Errorf("unexpected trailing tokens")
	}
	env := map[string]condValue{
		"event":    string(ctx.Event),
		"tool":     ctx.ToolName,
		"filePath": ctx.FilePath,
		"message":  ctx.Message,
	}
	if ctx.Args != nil {
		env["args"] = ctx.Args
	}
	value, err := evalCondNode(node, env)
	if err != nil {
		return false, err
	}
	return condTruthiness(value), nil
}
