// Package builtin provides built-in tools for the Orchestra tool system.
package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
)

// ---------------------------------------------------------------------------
// Calculator Tool
// ---------------------------------------------------------------------------

// CalculatorInput defines the input for the calculator tool.
type CalculatorInput struct {
	// Expression is the mathematical expression to evaluate.
	// Supports: +, -, *, /, %, ^ (power), parentheses, and functions.
	Expression string `json:"expression" description:"Mathematical expression to evaluate (e.g., '2 + 3 * 4', 'sqrt(16)', 'sin(pi/2)')"`
}

// CalculatorOutput defines the output of the calculator tool.
type CalculatorOutput struct {
	// Result is the numeric result of the evaluation.
	Result float64 `json:"result"`

	// Formatted is a human-readable string representation of the result.
	Formatted string `json:"formatted"`

	// Error contains an error message if evaluation failed.
	Error string `json:"error,omitempty"`
}

// NewCalculatorTool creates the calculator built-in tool.
//
// This tool safely evaluates mathematical expressions without using
// eval or exec. It implements a recursive descent parser that supports:
//   - Basic arithmetic: +, -, *, /, % (modulo), ^ (power)
//   - Parentheses for grouping
//   - Unary plus and minus
//   - Mathematical functions: sin, cos, tan, asin, acos, atan, sqrt, cbrt, abs,
//     ceil, floor, round, log (natural), log2, log10, exp
//   - Constants: pi, e, phi (golden ratio)
func NewCalculatorTool() CalculatorTool {
	return CalculatorTool{}
}

// CalculatorTool implements the calculator built-in tool.
// It is safe for concurrent use.
type CalculatorTool struct{}

// Name returns the tool's identifier.
func (t CalculatorTool) Name() string { return "calculator" }

// Description returns the tool's description for the LLM.
func (t CalculatorTool) Description() string {
	return `Evaluate mathematical expressions safely.

Supports basic arithmetic operators: +, -, *, /, % (modulo), ^ (power).
Use parentheses for grouping: (2 + 3) * 4

Mathematical functions: sin, cos, tan, asin, acos, atan, sqrt, cbrt, abs,
ceil, floor, round, log (natural log), log2, log10, exp

Constants: pi (3.14159...), e (2.71828...), phi (1.61803... golden ratio)

Examples:
- "2 + 3 * 4" → 14
- "(2 + 3) * 4" → 20
- "sqrt(144)" → 12
- "2 ^ 10" → 1024
- "sin(pi / 2)" → 1
- "log(e)" → 1
- "abs(-42)" → 42`
}

// Parameters returns the JSON Schema for the tool's input.
func (t CalculatorTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"expression": {
				"type": "string",
				"description": "Mathematical expression to evaluate (e.g., '2 + 3 * 4', 'sqrt(16)', 'sin(pi/2)')"
			}
		},
		"required": ["expression"]
	}`)
}

// Execute evaluates the mathematical expression.
func (t CalculatorTool) Execute(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req CalculatorInput
	if err := json.Unmarshal(input, &req); err != nil {
		return marshalCalcError(fmt.Errorf("parse input: %w", err))
	}

	if req.Expression == "" {
		return marshalCalcError(fmt.Errorf("expression is required"))
	}

	// Check for context cancellation
	if ctx.Err() != nil {
		return marshalCalcError(ctx.Err())
	}

	result, err := evaluateExpression(req.Expression)
	if err != nil {
		return marshalCalcError(err)
	}

	output := CalculatorOutput{
		Result:    result,
		Formatted: formatResult(result),
	}

	return json.Marshal(output)
}

// marshalCalcError creates a JSON error response for the calculator.
func marshalCalcError(err error) (json.RawMessage, error) {
	output := CalculatorOutput{
		Error: err.Error(),
	}
	return json.Marshal(output)
}

// ---------------------------------------------------------------------------
// Expression Parser (Recursive Descent)
// ---------------------------------------------------------------------------

// parser implements a recursive descent parser for mathematical expressions.
// Grammar (precedence low to high):
//
//	expr     → term (('+' | '-') term)*
//	term     → power (('*' | '/' | '%') power)*
//	power    → unary ('^' unary)*           (right-associative)
//	unary    → ('-' | '+') unary | call
//	call     → IDENTIFIER '(' expr ')' | primary
//	primary  → NUMBER | '(' expr ')' | IDENTIFIER (constant)
type parser struct {
	input  string
	pos    int
	length int
}

// parseError represents an error during expression parsing.
type parseError struct {
	pos    int
	msg    string
	input  string
}

func (e *parseError) Error() string {
	// Show context around the error position
	start := e.pos - 10
	if start < 0 {
		start = 0
	}
	end := e.pos + 10
	if end > len(e.input) {
		end = len(e.input)
	}
	context := e.input[start:end]
	return fmt.Sprintf("%s at position %d: ...%s...", e.msg, e.pos, context)
}

// evaluateExpression parses and evaluates a mathematical expression string.
func evaluateExpression(expr string) (float64, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return 0, fmt.Errorf("empty expression")
	}

	p := &parser{
		input:  expr,
		pos:    0,
		length: len(expr),
	}

	result, err := p.parseExpr()
	if err != nil {
		return 0, err
	}

	// Ensure we consumed all input
	p.skipWhitespace()
	if p.pos < p.length {
		return 0, &parseError{
			pos:   p.pos,
			msg:   "unexpected character after expression",
			input: expr,
		}
	}

	// Check for special values
	if math.IsInf(result, 0) {
		return 0, fmt.Errorf("result is infinity")
	}
	if math.IsNaN(result) {
		return 0, fmt.Errorf("result is not a number (NaN)")
	}

	return result, nil
}

// parseExpr handles addition and subtraction (lowest precedence).
func (p *parser) parseExpr() (float64, error) {
	left, err := p.parseTerm()
	if err != nil {
		return 0, err
	}

	for {
		p.skipWhitespace()
		if p.pos >= p.length {
			break
		}

		op := p.peek()
		if op != '+' && op != '-' {
			break
		}
		p.advance()

		right, err := p.parseTerm()
		if err != nil {
			return 0, err
		}

		switch op {
		case '+':
			left += right
		case '-':
			left -= right
		}
	}

	return left, nil
}

// parseTerm handles multiplication, division, and modulo.
func (p *parser) parseTerm() (float64, error) {
	left, err := p.parsePower()
	if err != nil {
		return 0, err
	}

	for {
		p.skipWhitespace()
		if p.pos >= p.length {
			break
		}

		op := p.peek()
		if op != '*' && op != '/' && op != '%' {
			break
		}
		p.advance()

		right, err := p.parsePower()
		if err != nil {
			return 0, err
		}

		switch op {
		case '*':
			left *= right
		case '/':
			if right == 0 {
				return 0, &parseError{
					pos:   p.pos - 1,
					msg:   "division by zero",
					input: p.input,
				}
			}
			left /= right
		case '%':
			if right == 0 {
				return 0, &parseError{
					pos:   p.pos - 1,
					msg:   "modulo by zero",
					input: p.input,
				}
			}
			left = math.Mod(left, right)
		}
	}

	return left, nil
}

// parsePower handles exponentiation (right-associative).
func (p *parser) parsePower() (float64, error) {
	base, err := p.parseUnary()
	if err != nil {
		return 0, err
	}

	p.skipWhitespace()
	if p.pos < p.length && p.peek() == '^' {
		p.advance()
		exp, err := p.parsePower() // Right-recursive for right-associativity
		if err != nil {
			return 0, err
		}
		return math.Pow(base, exp), nil
	}

	return base, nil
}

// parseUnary handles unary plus and minus.
func (p *parser) parseUnary() (float64, error) {
	p.skipWhitespace()
	if p.pos >= p.length {
		return 0, &parseError{
			pos:   p.pos,
			msg:   "unexpected end of expression",
			input: p.input,
		}
	}

	op := p.peek()
	if op == '-' || op == '+' {
		p.advance()
		val, err := p.parseUnary()
		if err != nil {
			return 0, err
		}
		if op == '-' {
			return -val, nil
		}
		return val, nil
	}

	return p.parseCall()
}

// parseCall handles function calls and constants.
func (p *parser) parseCall() (float64, error) {
	p.skipWhitespace()

	// Try to read an identifier
	start := p.pos
	for p.pos < p.length && (unicode.IsLetter(rune(p.input[p.pos])) || p.input[p.pos] == '_') {
		p.advance()
	}

	if p.pos == start {
		// No identifier found, parse as primary
		return p.parsePrimary()
	}

	name := strings.ToLower(p.input[start:p.pos])
	p.skipWhitespace()

	// Check if this is a function call (followed by '(')
	if p.pos < p.length && p.input[p.pos] == '(' {
		p.advance() // consume '('

		// Parse the argument
		arg, err := p.parseExpr()
		if err != nil {
			return 0, err
		}

		p.skipWhitespace()
		if p.pos >= p.length || p.input[p.pos] != ')' {
			return 0, &parseError{
				pos:   p.pos,
				msg:   "expected ')' after function argument",
				input: p.input,
			}
		}
		p.advance() // consume ')'

		// Evaluate the function
		result, ok := evalFunction(name, arg)
		if !ok {
			return 0, &parseError{
				pos:   start,
				msg:   fmt.Sprintf("unknown function: %s", name),
				input: p.input,
			}
		}
		return result, nil
	}

	// It's a constant
	result, ok := evalConstant(name)
	if !ok {
		return 0, &parseError{
			pos:   start,
			msg:   fmt.Sprintf("unknown identifier: %s", name),
			input: p.input,
		}
	}
	return result, nil
}

// parsePrimary handles numbers and parenthesized expressions.
func (p *parser) parsePrimary() (float64, error) {
	p.skipWhitespace()
	if p.pos >= p.length {
		return 0, &parseError{
			pos:   p.pos,
			msg:   "unexpected end of expression",
			input: p.input,
		}
	}

	ch := p.input[p.pos]

	// Parenthesized expression
	if ch == '(' {
		p.advance()
		result, err := p.parseExpr()
		if err != nil {
			return 0, err
		}

		p.skipWhitespace()
		if p.pos >= p.length || p.input[p.pos] != ')' {
			return 0, &parseError{
				pos:   p.pos,
				msg:   "expected ')' to close parenthesized expression",
				input: p.input,
			}
		}
		p.advance()
		return result, nil
	}

	// Number (integer or decimal)
	if ch == '.' || (ch >= '0' && ch <= '9') {
		return p.parseNumber()
	}

	return 0, &parseError{
		pos:   p.pos,
		msg:   fmt.Sprintf("unexpected character '%c'", ch),
		input: p.input,
	}
}

// parseNumber reads a numeric literal.
func (p *parser) parseNumber() (float64, error) {
	start := p.pos

	// Read digits before decimal point
	for p.pos < p.length && p.input[p.pos] >= '0' && p.input[p.pos] <= '9' {
		p.advance()
	}

	// Read decimal point and digits after
	if p.pos < p.length && p.input[p.pos] == '.' {
		p.advance()
		for p.pos < p.length && p.input[p.pos] >= '0' && p.input[p.pos] <= '9' {
			p.advance()
		}
	}

	// Read scientific notation
	if p.pos < p.length && (p.input[p.pos] == 'e' || p.input[p.pos] == 'E') {
		p.advance()
		if p.pos < p.length && (p.input[p.pos] == '+' || p.input[p.pos] == '-') {
			p.advance()
		}
		for p.pos < p.length && p.input[p.pos] >= '0' && p.input[p.pos] <= '9' {
			p.advance()
		}
	}

	numStr := p.input[start:p.pos]
	result, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0, &parseError{
			pos:   start,
			msg:   fmt.Sprintf("invalid number: %s", numStr),
			input: p.input,
		}
	}

	return result, nil
}

// peek returns the current character without advancing.
func (p *parser) peek() byte {
	if p.pos >= p.length {
		return 0
	}
	return p.input[p.pos]
}

// advance moves to the next character.
func (p *parser) advance() {
	p.pos++
}

// skipWhitespace skips whitespace characters.
func (p *parser) skipWhitespace() {
	for p.pos < p.length && (p.input[p.pos] == ' ' || p.input[p.pos] == '\t' || p.input[p.pos] == '\n' || p.input[p.pos] == '\r') {
		p.pos++
	}
}

// ---------------------------------------------------------------------------
// Functions and Constants
// ---------------------------------------------------------------------------

// mathFunctions maps function names to their implementations.
var mathFunctions = map[string]func(float64) float64{
	"sin":   math.Sin,
	"cos":   math.Cos,
	"tan":   math.Tan,
	"asin":  math.Asin,
	"acos":  math.Acos,
	"atan":  math.Atan,
	"sqrt":  math.Sqrt,
	"cbrt":  math.Cbrt,
	"abs":   math.Abs,
	"ceil":  math.Ceil,
	"floor": math.Floor,
	"round": math.Round,
	"log":   math.Log,
	"log2":  math.Log2,
	"log10": math.Log10,
	"exp":   math.Exp,
	"sign":  func(x float64) float64 { return math.Copysign(1, x) },
	"trunc": math.Trunc,
	"isnan": func(x float64) float64 {
		if math.IsNaN(x) {
			return 1
		}
		return 0
	},
	"isinf": func(x float64) float64 {
		if math.IsInf(x, 0) {
			return 1
		}
		return 0
	},
}

// evalFunction evaluates a named mathematical function.
// Returns (result, true) if the function exists, (0, false) otherwise.
func evalFunction(name string, arg float64) (float64, bool) {
	fn, ok := mathFunctions[name]
	if !ok {
		return 0, false
	}

	result := fn(arg)

	// Check for domain errors (e.g., sqrt of negative, log of negative)
	if math.IsNaN(result) && !math.IsNaN(arg) {
		// This is likely a domain error
		return 0, false
	}

	return result, true
}

// mathConstants maps constant names to their values.
var mathConstants = map[string]float64{
	"pi":  math.Pi,
	"e":   math.E,
	"phi": math.Phi, // Golden ratio
	"tau": 2 * math.Pi,
	"inf": math.Inf(1),
}

// evalConstant returns the value of a named mathematical constant.
// Returns (value, true) if the constant exists, (0, false) otherwise.
func evalConstant(name string) (float64, bool) {
	val, ok := mathConstants[name]
	return val, ok
}

// ---------------------------------------------------------------------------
// Result Formatting
// ---------------------------------------------------------------------------

// formatResult formats a float64 as a human-readable string.
// It tries to show integers without decimal points and limits
// precision for very large or very small numbers.
func formatResult(result float64) string {
	// Check if it's an integer value
	if result == math.Trunc(result) && !math.IsInf(result, 0) && math.Abs(result) < 1e15 {
		return strconv.FormatInt(int64(result), 10)
	}

	// For very small numbers close to zero
	if math.Abs(result) < 1e-10 && result != 0 {
		return fmt.Sprintf("%.2e", result)
	}

	// For very large numbers
	if math.Abs(result) >= 1e10 {
		return fmt.Sprintf("%.6e", result)
	}

	// Default: show up to 10 decimal places, trimming trailing zeros
	str := fmt.Sprintf("%.10f", result)
	str = strings.TrimRight(str, "0")
	str = strings.TrimRight(str, ".")
	return str
}
