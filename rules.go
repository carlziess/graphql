package graphql

import (
	"fmt"
	"github.com/graphql-go/graphql/gqlerrors"
	"github.com/graphql-go/graphql/language/ast"
	"github.com/graphql-go/graphql/language/kinds"
	"github.com/graphql-go/graphql/language/printer"
	"github.com/graphql-go/graphql/language/visitor"
	"sort"
	"strings"
)

/**
 * SpecifiedRules set includes all validation rules defined by the GraphQL spec.
 */
var SpecifiedRules = []ValidationRuleFn{
	ArgumentsOfCorrectTypeRule,
	DefaultValuesOfCorrectTypeRule,
	FieldsOnCorrectTypeRule,
	FragmentsOnCompositeTypesRule,
	KnownArgumentNamesRule,
	KnownDirectivesRule,
	KnownFragmentNamesRule,
	KnownTypeNamesRule,
	LoneAnonymousOperationRule,
	NoFragmentCyclesRule,
	NoUndefinedVariablesRule,
	NoUnusedFragmentsRule,
	NoUnusedVariablesRule,
	OverlappingFieldsCanBeMergedRule,
	PossibleFragmentSpreadsRule,
	ProvidedNonNullArgumentsRule,
	ScalarLeafsRule,
	UniqueArgumentNamesRule,
	UniqueFragmentNamesRule,
	UniqueInputFieldNamesRule,
	UniqueOperationNamesRule,
	UniqueVariableNamesRule,
	VariablesAreInputTypesRule,
	VariablesInAllowedPositionRule,
}

type ValidationRuleInstance struct {
	VisitorOpts          *visitor.VisitorOptions
	VisitSpreadFragments bool
}
type ValidationRuleFn func(context *ValidationContext) *ValidationRuleInstance

func newValidationError(message string, nodes []ast.Node) *gqlerrors.Error {
	return gqlerrors.NewError(
		message,
		nodes,
		"",
		nil,
		[]int{},
		nil, // TODO: this is interim, until we port "better-error-messages-for-inputs"
	)
}

func reportError(context *ValidationContext, message string, nodes []ast.Node) (string, interface{}) {
	context.ReportError(newValidationError(message, nodes))
	return visitor.ActionNoChange, nil
}

/**
 * ArgumentsOfCorrectTypeRule
 * Argument values of correct type
 *
 * A GraphQL document is only valid if all field argument literal values are
 * of the type expected by their position.
 */
func ArgumentsOfCorrectTypeRule(context *ValidationContext) *ValidationRuleInstance {
	visitorOpts := &visitor.VisitorOptions{
		KindFuncMap: map[string]visitor.NamedVisitFuncs{
			kinds.Argument: visitor.NamedVisitFuncs{
				Kind: func(p visitor.VisitFuncParams) (string, interface{}) {
					var action = visitor.ActionNoChange
					var result interface{}
					if argAST, ok := p.Node.(*ast.Argument); ok {
						value := argAST.Value
						argDef := context.Argument()
						isValid, messages := isValidLiteralValue(argDef.Type, value)
						if argDef != nil && !isValid {
							argNameValue := ""
							if argAST.Name != nil {
								argNameValue = argAST.Name.Value
							}

							messagesStr := ""
							if len(messages) > 0 {
								messagesStr = "\n" + strings.Join(messages, "\n")
							}
							return reportError(
								context,
								fmt.Sprintf(`Argument "%v" has invalid value %v.%v`,
									argNameValue, printer.Print(value), messagesStr),
								[]ast.Node{value},
							)
						}
					}
					return action, result
				},
			},
		},
	}
	return &ValidationRuleInstance{
		VisitorOpts: visitorOpts,
	}
}

/**
 * DefaultValuesOfCorrectTypeRule
 * Variable default values of correct type
 *
 * A GraphQL document is only valid if all variable default values are of the
 * type expected by their definition.
 */
func DefaultValuesOfCorrectTypeRule(context *ValidationContext) *ValidationRuleInstance {
	visitorOpts := &visitor.VisitorOptions{
		KindFuncMap: map[string]visitor.NamedVisitFuncs{
			kinds.VariableDefinition: visitor.NamedVisitFuncs{
				Kind: func(p visitor.VisitFuncParams) (string, interface{}) {
					var action = visitor.ActionNoChange
					var result interface{}
					if varDefAST, ok := p.Node.(*ast.VariableDefinition); ok {
						name := ""
						if varDefAST.Variable != nil && varDefAST.Variable.Name != nil {
							name = varDefAST.Variable.Name.Value
						}
						defaultValue := varDefAST.DefaultValue
						ttype := context.InputType()

						if ttype, ok := ttype.(*NonNull); ok && defaultValue != nil {
							return reportError(
								context,
								fmt.Sprintf(`Variable "$%v" of type "%v" is required and will not use the default value. Perhaps you meant to use type "%v".`,
									name, ttype, ttype.OfType),
								[]ast.Node{defaultValue},
							)
						}
						isValid, messages := isValidLiteralValue(ttype, defaultValue)
						if ttype != nil && defaultValue != nil && !isValid {
							messagesStr := ""
							if len(messages) > 0 {
								messagesStr = "\n" + strings.Join(messages, "\n")
							}
							return reportError(
								context,
								fmt.Sprintf(`Variable "$%v" has invalid default value: %v.%v`,
									name, printer.Print(defaultValue), messagesStr),
								[]ast.Node{defaultValue},
							)
						}
					}
					return action, result
				},
			},
		},
	}
	return &ValidationRuleInstance{
		VisitorOpts: visitorOpts,
	}
}

/**
 * FieldsOnCorrectTypeRule
 * Fields on correct type
 *
 * A GraphQL document is only valid if all fields selected are defined by the
 * parent type, or are an allowed meta field such as __typenamme
 */
func FieldsOnCorrectTypeRule(context *ValidationContext) *ValidationRuleInstance {
	visitorOpts := &visitor.VisitorOptions{
		KindFuncMap: map[string]visitor.NamedVisitFuncs{
			kinds.Field: visitor.NamedVisitFuncs{
				Kind: func(p visitor.VisitFuncParams) (string, interface{}) {
					var action = visitor.ActionNoChange
					var result interface{}
					if node, ok := p.Node.(*ast.Field); ok {
						ttype := context.ParentType()

						if ttype != nil {
							fieldDef := context.FieldDef()
							if fieldDef == nil {
								nodeName := ""
								if node.Name != nil {
									nodeName = node.Name.Value
								}
								return reportError(
									context,
									fmt.Sprintf(`Cannot query field "%v" on "%v".`,
										nodeName, ttype.Name()),
									[]ast.Node{node},
								)
							}
						}
					}
					return action, result
				},
			},
		},
	}
	return &ValidationRuleInstance{
		VisitorOpts: visitorOpts,
	}
}

/**
 * FragmentsOnCompositeTypesRule
 * Fragments on composite type
 *
 * Fragments use a type condition to determine if they apply, since fragments
 * can only be spread into a composite type (object, interface, or union), the
 * type condition must also be a composite type.
 */
func FragmentsOnCompositeTypesRule(context *ValidationContext) *ValidationRuleInstance {
	visitorOpts := &visitor.VisitorOptions{
		KindFuncMap: map[string]visitor.NamedVisitFuncs{
			kinds.InlineFragment: visitor.NamedVisitFuncs{
				Kind: func(p visitor.VisitFuncParams) (string, interface{}) {
					if node, ok := p.Node.(*ast.InlineFragment); ok {
						ttype := context.Type()
						if ttype != nil && !IsCompositeType(ttype) {
							return reportError(
								context,
								fmt.Sprintf(`Fragment cannot condition on non composite type "%v".`, ttype),
								[]ast.Node{node.TypeCondition},
							)
						}
					}
					return visitor.ActionNoChange, nil
				},
			},
			kinds.FragmentDefinition: visitor.NamedVisitFuncs{
				Kind: func(p visitor.VisitFuncParams) (string, interface{}) {
					if node, ok := p.Node.(*ast.FragmentDefinition); ok {
						ttype := context.Type()
						if ttype != nil && !IsCompositeType(ttype) {
							nodeName := ""
							if node.Name != nil {
								nodeName = node.Name.Value
							}
							return reportError(
								context,
								fmt.Sprintf(`Fragment "%v" cannot condition on non composite type "%v".`, nodeName, printer.Print(node.TypeCondition)),
								[]ast.Node{node.TypeCondition},
							)
						}
					}
					return visitor.ActionNoChange, nil
				},
			},
		},
	}
	return &ValidationRuleInstance{
		VisitorOpts: visitorOpts,
	}
}

/**
 * KnownArgumentNamesRule
 * Known argument names
 *
 * A GraphQL field is only valid if all supplied arguments are defined by
 * that field.
 */
func KnownArgumentNamesRule(context *ValidationContext) *ValidationRuleInstance {
	visitorOpts := &visitor.VisitorOptions{
		KindFuncMap: map[string]visitor.NamedVisitFuncs{
			kinds.Argument: visitor.NamedVisitFuncs{
				Kind: func(p visitor.VisitFuncParams) (string, interface{}) {
					var action = visitor.ActionNoChange
					var result interface{}
					if node, ok := p.Node.(*ast.Argument); ok {
						var argumentOf ast.Node
						if len(p.Ancestors) > 0 {
							argumentOf = p.Ancestors[len(p.Ancestors)-1]
						}
						if argumentOf == nil {
							return action, result
						}
						if argumentOf.GetKind() == "Field" {
							fieldDef := context.FieldDef()
							if fieldDef == nil {
								return action, result
							}
							nodeName := ""
							if node.Name != nil {
								nodeName = node.Name.Value
							}
							var fieldArgDef *Argument
							for _, arg := range fieldDef.Args {
								if arg.Name() == nodeName {
									fieldArgDef = arg
								}
							}
							if fieldArgDef == nil {
								parentType := context.ParentType()
								parentTypeName := ""
								if parentType != nil {
									parentTypeName = parentType.Name()
								}
								return reportError(
									context,
									fmt.Sprintf(`Unknown argument "%v" on field "%v" of type "%v".`, nodeName, fieldDef.Name, parentTypeName),
									[]ast.Node{node},
								)
							}
						} else if argumentOf.GetKind() == "Directive" {
							directive := context.Directive()
							if directive == nil {
								return action, result
							}
							nodeName := ""
							if node.Name != nil {
								nodeName = node.Name.Value
							}
							var directiveArgDef *Argument
							for _, arg := range directive.Args {
								if arg.Name() == nodeName {
									directiveArgDef = arg
								}
							}
							if directiveArgDef == nil {
								return reportError(
									context,
									fmt.Sprintf(`Unknown argument "%v" on directive "@%v".`, nodeName, directive.Name),
									[]ast.Node{node},
								)
							}
						}

					}
					return action, result
				},
			},
		},
	}
	return &ValidationRuleInstance{
		VisitorOpts: visitorOpts,
	}
}

/**
 * Known directives
 *
 * A GraphQL document is only valid if all `@directives` are known by the
 * schema and legally positioned.
 */
func KnownDirectivesRule(context *ValidationContext) *ValidationRuleInstance {
	visitorOpts := &visitor.VisitorOptions{
		KindFuncMap: map[string]visitor.NamedVisitFuncs{
			kinds.Directive: visitor.NamedVisitFuncs{
				Kind: func(p visitor.VisitFuncParams) (string, interface{}) {
					var action = visitor.ActionNoChange
					var result interface{}
					if node, ok := p.Node.(*ast.Directive); ok {

						nodeName := ""
						if node.Name != nil {
							nodeName = node.Name.Value
						}

						var directiveDef *Directive
						for _, def := range context.Schema().Directives() {
							if def.Name == nodeName {
								directiveDef = def
							}
						}
						if directiveDef == nil {
							return reportError(
								context,
								fmt.Sprintf(`Unknown directive "%v".`, nodeName),
								[]ast.Node{node},
							)
						}

						var appliedTo ast.Node
						if len(p.Ancestors) > 0 {
							appliedTo = p.Ancestors[len(p.Ancestors)-1]
						}
						if appliedTo == nil {
							return action, result
						}

						if appliedTo.GetKind() == kinds.OperationDefinition && directiveDef.OnOperation == false {
							return reportError(
								context,
								fmt.Sprintf(`Directive "%v" may not be used on "%v".`, nodeName, "operation"),
								[]ast.Node{node},
							)
						}
						if appliedTo.GetKind() == kinds.Field && directiveDef.OnField == false {
							return reportError(
								context,
								fmt.Sprintf(`Directive "%v" may not be used on "%v".`, nodeName, "field"),
								[]ast.Node{node},
							)
						}
						if (appliedTo.GetKind() == kinds.FragmentSpread ||
							appliedTo.GetKind() == kinds.InlineFragment ||
							appliedTo.GetKind() == kinds.FragmentDefinition) && directiveDef.OnFragment == false {
							return reportError(
								context,
								fmt.Sprintf(`Directive "%v" may not be used on "%v".`, nodeName, "fragment"),
								[]ast.Node{node},
							)
						}

					}
					return action, result
				},
			},
		},
	}
	return &ValidationRuleInstance{
		VisitorOpts: visitorOpts,
	}
}

/**
 * KnownFragmentNamesRule
 * Known fragment names
 *
 * A GraphQL document is only valid if all `...Fragment` fragment spreads refer
 * to fragments defined in the same document.
 */
func KnownFragmentNamesRule(context *ValidationContext) *ValidationRuleInstance {
	visitorOpts := &visitor.VisitorOptions{
		KindFuncMap: map[string]visitor.NamedVisitFuncs{
			kinds.FragmentSpread: visitor.NamedVisitFuncs{
				Kind: func(p visitor.VisitFuncParams) (string, interface{}) {
					var action = visitor.ActionNoChange
					var result interface{}
					if node, ok := p.Node.(*ast.FragmentSpread); ok {

						fragmentName := ""
						if node.Name != nil {
							fragmentName = node.Name.Value
						}

						fragment := context.Fragment(fragmentName)
						if fragment == nil {
							return reportError(
								context,
								fmt.Sprintf(`Unknown fragment "%v".`, fragmentName),
								[]ast.Node{node.Name},
							)
						}
					}
					return action, result
				},
			},
		},
	}
	return &ValidationRuleInstance{
		VisitorOpts: visitorOpts,
	}
}

/**
 * KnownTypeNamesRule
 * Known type names
 *
 * A GraphQL document is only valid if referenced types (specifically
 * variable definitions and fragment conditions) are defined by the type schema.
 */
func KnownTypeNamesRule(context *ValidationContext) *ValidationRuleInstance {
	visitorOpts := &visitor.VisitorOptions{
		KindFuncMap: map[string]visitor.NamedVisitFuncs{
			kinds.Named: visitor.NamedVisitFuncs{
				Kind: func(p visitor.VisitFuncParams) (string, interface{}) {
					if node, ok := p.Node.(*ast.Named); ok {
						typeNameValue := ""
						typeName := node.Name
						if typeName != nil {
							typeNameValue = typeName.Value
						}
						ttype := context.Schema().Type(typeNameValue)
						if ttype == nil {
							return reportError(
								context,
								fmt.Sprintf(`Unknown type "%v".`, typeNameValue),
								[]ast.Node{node},
							)
						}
					}
					return visitor.ActionNoChange, nil
				},
			},
		},
	}
	return &ValidationRuleInstance{
		VisitorOpts: visitorOpts,
	}
}

/**
 * LoneAnonymousOperationRule
 * Lone anonymous operation
 *
 * A GraphQL document is only valid if when it contains an anonymous operation
 * (the query short-hand) that it contains only that one operation definition.
 */
func LoneAnonymousOperationRule(context *ValidationContext) *ValidationRuleInstance {
	var operationCount = 0
	visitorOpts := &visitor.VisitorOptions{
		KindFuncMap: map[string]visitor.NamedVisitFuncs{
			kinds.Document: visitor.NamedVisitFuncs{
				Kind: func(p visitor.VisitFuncParams) (string, interface{}) {
					if node, ok := p.Node.(*ast.Document); ok {
						operationCount = 0
						for _, definition := range node.Definitions {
							if definition.GetKind() == kinds.OperationDefinition {
								operationCount = operationCount + 1
							}
						}
					}
					return visitor.ActionNoChange, nil
				},
			},
			kinds.OperationDefinition: visitor.NamedVisitFuncs{
				Kind: func(p visitor.VisitFuncParams) (string, interface{}) {
					if node, ok := p.Node.(*ast.OperationDefinition); ok {
						if node.Name == nil && operationCount > 1 {
							return reportError(
								context,
								`This anonymous operation must be the only defined operation.`,
								[]ast.Node{node},
							)
						}
					}
					return visitor.ActionNoChange, nil
				},
			},
		},
	}
	return &ValidationRuleInstance{
		VisitorOpts: visitorOpts,
	}
}

type nodeSet struct {
	set map[ast.Node]bool
}

func newNodeSet() *nodeSet {
	return &nodeSet{
		set: map[ast.Node]bool{},
	}
}
func (set *nodeSet) Has(node ast.Node) bool {
	_, ok := set.set[node]
	return ok
}
func (set *nodeSet) Add(node ast.Node) bool {
	if set.Has(node) {
		return false
	}
	set.set[node] = true
	return true
}

/**
 * NoFragmentCyclesRule
 */
func NoFragmentCyclesRule(context *ValidationContext) *ValidationRuleInstance {
	// Gather all the fragment spreads ASTs for each fragment definition.
	// Importantly this does not include inline fragments.
	definitions := context.Document().Definitions
	spreadsInFragment := map[string][]*ast.FragmentSpread{}
	for _, node := range definitions {
		if node.GetKind() == kinds.FragmentDefinition {
			if node, ok := node.(*ast.FragmentDefinition); ok && node != nil {
				nodeName := ""
				if node.Name != nil {
					nodeName = node.Name.Value
				}
				spreadsInFragment[nodeName] = gatherSpreads(node)
			}
		}
	}
	// Tracks spreads known to lead to cycles to ensure that cycles are not
	// redundantly reported.
	knownToLeadToCycle := newNodeSet()

	visitorOpts := &visitor.VisitorOptions{
		KindFuncMap: map[string]visitor.NamedVisitFuncs{
			kinds.FragmentDefinition: visitor.NamedVisitFuncs{
				Kind: func(p visitor.VisitFuncParams) (string, interface{}) {
					if node, ok := p.Node.(*ast.FragmentDefinition); ok && node != nil {
						spreadPath := []*ast.FragmentSpread{}
						initialName := ""
						if node.Name != nil {
							initialName = node.Name.Value
						}
						var detectCycleRecursive func(fragmentName string)
						detectCycleRecursive = func(fragmentName string) {
							spreadNodes, _ := spreadsInFragment[fragmentName]
							for _, spreadNode := range spreadNodes {
								if knownToLeadToCycle.Has(spreadNode) {
									continue
								}
								spreadNodeName := ""
								if spreadNode.Name != nil {
									spreadNodeName = spreadNode.Name.Value
								}
								if spreadNodeName == initialName {
									cyclePath := []ast.Node{}
									for _, path := range spreadPath {
										cyclePath = append(cyclePath, path)
									}
									cyclePath = append(cyclePath, spreadNode)
									for _, spread := range cyclePath {
										knownToLeadToCycle.Add(spread)
									}
									via := ""
									spreadNames := []string{}
									for _, s := range spreadPath {
										if s.Name != nil {
											spreadNames = append(spreadNames, s.Name.Value)
										}
									}
									if len(spreadNames) > 0 {
										via = " via " + strings.Join(spreadNames, ", ")
									}
									reportError(
										context,
										fmt.Sprintf(`Cannot spread fragment "%v" within itself%v.`, initialName, via),
										cyclePath,
									)
									continue
								}
								spreadPathHasCurrentNode := false
								for _, spread := range spreadPath {
									if spread == spreadNode {
										spreadPathHasCurrentNode = true
									}
								}
								if spreadPathHasCurrentNode {
									continue
								}
								spreadPath = append(spreadPath, spreadNode)
								detectCycleRecursive(spreadNodeName)
								_, spreadPath = spreadPath[len(spreadPath)-1], spreadPath[:len(spreadPath)-1]
							}
						}
						detectCycleRecursive(initialName)
					}
					return visitor.ActionNoChange, nil
				},
			},
		},
	}
	return &ValidationRuleInstance{
		VisitorOpts: visitorOpts,
	}
}

/**
 * NoUndefinedVariables
 * No undefined variables
 *
 * A GraphQL operation is only valid if all variables encountered, both directly
 * and via fragment spreads, are defined by that operation.
 */
func NoUndefinedVariablesRule(context *ValidationContext) *ValidationRuleInstance {
	var variableNameDefined = map[string]bool{}

	visitorOpts := &visitor.VisitorOptions{
		KindFuncMap: map[string]visitor.NamedVisitFuncs{
			kinds.OperationDefinition: visitor.NamedVisitFuncs{
				Enter: func(p visitor.VisitFuncParams) (string, interface{}) {
					variableNameDefined = map[string]bool{}
					return visitor.ActionNoChange, nil
				},
				Leave: func(p visitor.VisitFuncParams) (string, interface{}) {
					if operation, ok := p.Node.(*ast.OperationDefinition); ok && operation != nil {
						usages := context.RecursiveVariableUsages(operation)

						for _, usage := range usages {
							if usage == nil {
								continue
							}
							if usage.Node == nil {
								continue
							}
							varName := ""
							if usage.Node.Name != nil {
								varName = usage.Node.Name.Value
							}
							opName := ""
							if operation.Name != nil {
								opName = operation.Name.Value
							}
							if res, ok := variableNameDefined[varName]; !ok || !res {
								if opName != "" {
									reportError(
										context,
										fmt.Sprintf(`Variable "$%v" is not defined by operation "%v".`, varName, opName),
										[]ast.Node{usage.Node, operation},
									)
								} else {

									reportError(
										context,
										fmt.Sprintf(`Variable "$%v" is not defined.`, varName),
										[]ast.Node{usage.Node, operation},
									)
								}
							}
						}
					}
					return visitor.ActionNoChange, nil
				},
			},
			kinds.VariableDefinition: visitor.NamedVisitFuncs{
				Kind: func(p visitor.VisitFuncParams) (string, interface{}) {
					if node, ok := p.Node.(*ast.VariableDefinition); ok && node != nil {
						variableName := ""
						if node.Variable != nil && node.Variable.Name != nil {
							variableName = node.Variable.Name.Value
						}
						//						definedVariableNames[variableName] = true
						variableNameDefined[variableName] = true
					}
					return visitor.ActionNoChange, nil
				},
			},
		},
	}
	return &ValidationRuleInstance{
		VisitSpreadFragments: true,
		VisitorOpts:          visitorOpts,
	}
}

/**
 * NoUnusedFragmentsRule
 * No unused fragments
 *
 * A GraphQL document is only valid if all fragment definitions are spread
 * within operations, or spread within other fragments spread within operations.
 */
func NoUnusedFragmentsRule(context *ValidationContext) *ValidationRuleInstance {

	var fragmentDefs = []*ast.FragmentDefinition{}
	var spreadsWithinOperation = []map[string]bool{}
	var fragAdjacencies = map[string]map[string]bool{}
	var spreadNames = map[string]bool{}

	visitorOpts := &visitor.VisitorOptions{
		KindFuncMap: map[string]visitor.NamedVisitFuncs{
			kinds.OperationDefinition: visitor.NamedVisitFuncs{
				Kind: func(p visitor.VisitFuncParams) (string, interface{}) {
					if node, ok := p.Node.(*ast.OperationDefinition); ok && node != nil {
						spreadNames = map[string]bool{}
						spreadsWithinOperation = append(spreadsWithinOperation, spreadNames)
					}
					return visitor.ActionNoChange, nil
				},
			},
			kinds.FragmentDefinition: visitor.NamedVisitFuncs{
				Kind: func(p visitor.VisitFuncParams) (string, interface{}) {
					if def, ok := p.Node.(*ast.FragmentDefinition); ok && def != nil {
						defName := ""
						if def.Name != nil {
							defName = def.Name.Value
						}

						fragmentDefs = append(fragmentDefs, def)
						spreadNames = map[string]bool{}
						fragAdjacencies[defName] = spreadNames
					}
					return visitor.ActionNoChange, nil
				},
			},
			kinds.FragmentSpread: visitor.NamedVisitFuncs{
				Kind: func(p visitor.VisitFuncParams) (string, interface{}) {
					if spread, ok := p.Node.(*ast.FragmentSpread); ok && spread != nil {
						spreadName := ""
						if spread.Name != nil {
							spreadName = spread.Name.Value
						}
						spreadNames[spreadName] = true
					}
					return visitor.ActionNoChange, nil
				},
			},
			kinds.Document: visitor.NamedVisitFuncs{
				Leave: func(p visitor.VisitFuncParams) (string, interface{}) {

					fragmentNameUsed := map[string]interface{}{}

					var reduceSpreadFragments func(spreads map[string]bool)
					reduceSpreadFragments = func(spreads map[string]bool) {
						for fragName, _ := range spreads {
							if isFragNameUsed, _ := fragmentNameUsed[fragName]; isFragNameUsed != true {
								fragmentNameUsed[fragName] = true

								if adjacencies, ok := fragAdjacencies[fragName]; ok {
									reduceSpreadFragments(adjacencies)
								}
							}
						}
					}
					for _, spreadWithinOperation := range spreadsWithinOperation {
						reduceSpreadFragments(spreadWithinOperation)
					}
					for _, def := range fragmentDefs {
						defName := ""
						if def.Name != nil {
							defName = def.Name.Value
						}

						isFragNameUsed, ok := fragmentNameUsed[defName]
						if !ok || isFragNameUsed != true {
							reportError(
								context,
								fmt.Sprintf(`Fragment "%v" is never used.`, defName),
								[]ast.Node{def},
							)
						}
					}
					return visitor.ActionNoChange, nil
				},
			},
		},
	}
	return &ValidationRuleInstance{
		VisitorOpts: visitorOpts,
	}
}

/**
 * NoUnusedVariablesRule
 * No unused variables
 *
 * A GraphQL operation is only valid if all variables defined by an operation
 * are used, either directly or within a spread fragment.
 */
func NoUnusedVariablesRule(context *ValidationContext) *ValidationRuleInstance {

	var variableDefs = []*ast.VariableDefinition{}

	visitorOpts := &visitor.VisitorOptions{
		KindFuncMap: map[string]visitor.NamedVisitFuncs{
			kinds.OperationDefinition: visitor.NamedVisitFuncs{
				Enter: func(p visitor.VisitFuncParams) (string, interface{}) {
					variableDefs = []*ast.VariableDefinition{}
					return visitor.ActionNoChange, nil
				},
				Leave: func(p visitor.VisitFuncParams) (string, interface{}) {
					if operation, ok := p.Node.(*ast.OperationDefinition); ok && operation != nil {
						variableNameUsed := map[string]bool{}
						usages := context.RecursiveVariableUsages(operation)

						for _, usage := range usages {
							varName := ""
							if usage != nil && usage.Node != nil && usage.Node.Name != nil {
								varName = usage.Node.Name.Value
							}
							if varName != "" {
								variableNameUsed[varName] = true
							}
						}
						for _, variableDef := range variableDefs {
							variableName := ""
							if variableDef != nil && variableDef.Variable != nil && variableDef.Variable.Name != nil {
								variableName = variableDef.Variable.Name.Value
							}
							if res, ok := variableNameUsed[variableName]; !ok || !res {
								reportError(
									context,
									fmt.Sprintf(`Variable "$%v" is never used.`, variableName),
									[]ast.Node{variableDef},
								)
							}
						}

					}

					return visitor.ActionNoChange, nil
				},
			},
			kinds.VariableDefinition: visitor.NamedVisitFuncs{
				Kind: func(p visitor.VisitFuncParams) (string, interface{}) {
					if def, ok := p.Node.(*ast.VariableDefinition); ok && def != nil {
						variableDefs = append(variableDefs, def)
					}
					return visitor.ActionNoChange, nil
				},
			},
		},
	}
	return &ValidationRuleInstance{
		// Visit FragmentDefinition after visiting FragmentSpread
		VisitSpreadFragments: true,
		VisitorOpts:          visitorOpts,
	}
}

type fieldDefPair struct {
	Field    *ast.Field
	FieldDef *FieldDefinition
}

func collectFieldASTsAndDefs(context *ValidationContext, parentType Named, selectionSet *ast.SelectionSet, visitedFragmentNames map[string]bool, astAndDefs map[string][]*fieldDefPair) map[string][]*fieldDefPair {

	if astAndDefs == nil {
		astAndDefs = map[string][]*fieldDefPair{}
	}
	if visitedFragmentNames == nil {
		visitedFragmentNames = map[string]bool{}
	}
	if selectionSet == nil {
		return astAndDefs
	}
	for _, selection := range selectionSet.Selections {
		switch selection := selection.(type) {
		case *ast.Field:
			fieldName := ""
			if selection.Name != nil {
				fieldName = selection.Name.Value
			}
			var fieldDef *FieldDefinition
			if parentType, ok := parentType.(*Object); ok {
				fieldDef, _ = parentType.Fields()[fieldName]
			}
			if parentType, ok := parentType.(*Interface); ok {
				fieldDef, _ = parentType.Fields()[fieldName]
			}

			responseName := fieldName
			if selection.Alias != nil {
				responseName = selection.Alias.Value
			}
			_, ok := astAndDefs[responseName]
			if !ok {
				astAndDefs[responseName] = []*fieldDefPair{}
			}
			astAndDefs[responseName] = append(astAndDefs[responseName], &fieldDefPair{
				Field:    selection,
				FieldDef: fieldDef,
			})
		case *ast.InlineFragment:
			parentType, _ := typeFromAST(*context.Schema(), selection.TypeCondition)
			astAndDefs = collectFieldASTsAndDefs(
				context,
				parentType,
				selection.SelectionSet,
				visitedFragmentNames,
				astAndDefs,
			)
		case *ast.FragmentSpread:
			fragName := ""
			if selection.Name != nil {
				fragName = selection.Name.Value
			}
			if _, ok := visitedFragmentNames[fragName]; ok {
				continue
			}
			visitedFragmentNames[fragName] = true
			fragment := context.Fragment(fragName)
			if fragment == nil {
				continue
			}
			parentType, _ := typeFromAST(*context.Schema(), fragment.TypeCondition)
			astAndDefs = collectFieldASTsAndDefs(
				context,
				parentType,
				fragment.SelectionSet,
				visitedFragmentNames,
				astAndDefs,
			)
		}
	}
	return astAndDefs
}

/**
 * pairSet A way to keep track of pairs of things when the ordering of the pair does
 * not matter. We do this by maintaining a sort of double adjacency sets.
 */
type pairSet struct {
	data map[ast.Node]*nodeSet
}

func newPairSet() *pairSet {
	return &pairSet{
		data: map[ast.Node]*nodeSet{},
	}
}
func (pair *pairSet) Has(a ast.Node, b ast.Node) bool {
	first, ok := pair.data[a]
	if !ok || first == nil {
		return false
	}
	res := first.Has(b)
	return res
}
func (pair *pairSet) Add(a ast.Node, b ast.Node) bool {
	pair.data = pairSetAdd(pair.data, a, b)
	pair.data = pairSetAdd(pair.data, b, a)
	return true
}

func pairSetAdd(data map[ast.Node]*nodeSet, a, b ast.Node) map[ast.Node]*nodeSet {
	set, ok := data[a]
	if !ok || set == nil {
		set = newNodeSet()
		data[a] = set
	}
	set.Add(b)
	return data
}

type conflictReason struct {
	Name    string
	Message interface{} // conflictReason || []conflictReason
}
type conflict struct {
	Reason conflictReason
	Fields []ast.Node
}

func sameDirectives(directives1 []*ast.Directive, directives2 []*ast.Directive) bool {
	if len(directives1) != len(directives1) {
		return false
	}
	for _, directive1 := range directives1 {
		directive1Name := ""
		if directive1.Name != nil {
			directive1Name = directive1.Name.Value
		}

		var foundDirective2 *ast.Directive
		for _, directive2 := range directives2 {
			directive2Name := ""
			if directive2.Name != nil {
				directive2Name = directive2.Name.Value
			}
			if directive1Name == directive2Name {
				foundDirective2 = directive2
			}
			break
		}
		if foundDirective2 == nil {
			return false
		}
		if sameArguments(directive1.Arguments, foundDirective2.Arguments) == false {
			return false
		}
	}

	return true
}
func sameArguments(args1 []*ast.Argument, args2 []*ast.Argument) bool {
	if len(args1) != len(args2) {
		return false
	}

	for _, arg1 := range args1 {
		arg1Name := ""
		if arg1.Name != nil {
			arg1Name = arg1.Name.Value
		}

		var foundArgs2 *ast.Argument
		for _, arg2 := range args2 {
			arg2Name := ""
			if arg2.Name != nil {
				arg2Name = arg2.Name.Value
			}
			if arg1Name == arg2Name {
				foundArgs2 = arg2
			}
			break
		}
		if foundArgs2 == nil {
			return false
		}
		if sameValue(arg1.Value, foundArgs2.Value) == false {
			return false
		}
	}

	return true
}
func sameValue(value1 ast.Value, value2 ast.Value) bool {
	if value1 == nil && value2 == nil {
		return true
	}
	val1 := printer.Print(value1)
	val2 := printer.Print(value2)

	return val1 == val2
}

/**
 * OverlappingFieldsCanBeMergedRule
 * Overlapping fields can be merged
 *
 * A selection set is only valid if all fields (including spreading any
 * fragments) either correspond to distinct response names or can be merged
 * without ambiguity.
 */
func OverlappingFieldsCanBeMergedRule(context *ValidationContext) *ValidationRuleInstance {

	comparedSet := newPairSet()
	var findConflicts func(fieldMap map[string][]*fieldDefPair) (conflicts []*conflict)
	findConflict := func(responseName string, pair *fieldDefPair, pair2 *fieldDefPair) *conflict {

		ast1 := pair.Field
		def1 := pair.FieldDef

		ast2 := pair2.Field
		def2 := pair2.FieldDef

		if ast1 == ast2 || comparedSet.Has(ast1, ast2) {
			return nil
		}
		comparedSet.Add(ast1, ast2)

		name1 := ""
		if ast1.Name != nil {
			name1 = ast1.Name.Value
		}
		name2 := ""
		if ast2.Name != nil {
			name2 = ast2.Name.Value
		}
		if name1 != name2 {
			return &conflict{
				Reason: conflictReason{
					Name:    responseName,
					Message: fmt.Sprintf(`%v and %v are different fields`, name1, name2),
				},
				Fields: []ast.Node{ast1, ast2},
			}
		}

		var type1 Type
		var type2 Type
		if def1 != nil {
			type1 = def1.Type
		}
		if def2 != nil {
			type2 = def2.Type
		}

		if type1 != nil && type2 != nil && !isEqualType(type1, type2) {
			return &conflict{
				Reason: conflictReason{
					Name:    responseName,
					Message: fmt.Sprintf(`they return differing types %v and %v`, type1, type2),
				},
				Fields: []ast.Node{ast1, ast2},
			}
		}
		if !sameArguments(ast1.Arguments, ast2.Arguments) {
			return &conflict{
				Reason: conflictReason{
					Name:    responseName,
					Message: `they have differing arguments`,
				},
				Fields: []ast.Node{ast1, ast2},
			}
		}
		if !sameDirectives(ast1.Directives, ast2.Directives) {
			return &conflict{
				Reason: conflictReason{
					Name:    responseName,
					Message: `they have differing directives`,
				},
				Fields: []ast.Node{ast1, ast2},
			}
		}

		selectionSet1 := ast1.SelectionSet
		selectionSet2 := ast2.SelectionSet
		if selectionSet1 != nil && selectionSet2 != nil {
			visitedFragmentNames := map[string]bool{}
			subfieldMap := collectFieldASTsAndDefs(
				context,
				GetNamed(type1),
				selectionSet1,
				visitedFragmentNames,
				nil,
			)
			subfieldMap = collectFieldASTsAndDefs(
				context,
				GetNamed(type2),
				selectionSet2,
				visitedFragmentNames,
				subfieldMap,
			)
			conflicts := findConflicts(subfieldMap)
			if len(conflicts) > 0 {

				conflictReasons := []conflictReason{}
				conflictFields := []ast.Node{ast1, ast2}
				for _, c := range conflicts {
					conflictReasons = append(conflictReasons, c.Reason)
					conflictFields = append(conflictFields, c.Fields...)
				}

				return &conflict{
					Reason: conflictReason{
						Name:    responseName,
						Message: conflictReasons,
					},
					Fields: conflictFields,
				}
			}
		}
		return nil
	}

	findConflicts = func(fieldMap map[string][]*fieldDefPair) (conflicts []*conflict) {

		// ensure field traversal
		orderedName := sort.StringSlice{}
		for responseName, _ := range fieldMap {
			orderedName = append(orderedName, responseName)
		}
		orderedName.Sort()

		for _, responseName := range orderedName {
			fields, _ := fieldMap[responseName]
			for _, fieldA := range fields {
				for _, fieldB := range fields {
					c := findConflict(responseName, fieldA, fieldB)
					if c != nil {
						conflicts = append(conflicts, c)
					}
				}
			}
		}
		return conflicts
	}

	var reasonMessage func(message interface{}) string
	reasonMessage = func(message interface{}) string {
		switch reason := message.(type) {
		case string:
			return reason
		case conflictReason:
			return reasonMessage(reason.Message)
		case []conflictReason:
			messages := []string{}
			for _, r := range reason {
				messages = append(messages, fmt.Sprintf(
					`subfields "%v" conflict because %v`,
					r.Name,
					reasonMessage(r.Message),
				))
			}
			return strings.Join(messages, " and ")
		}
		return ""
	}

	visitorOpts := &visitor.VisitorOptions{
		KindFuncMap: map[string]visitor.NamedVisitFuncs{
			kinds.SelectionSet: visitor.NamedVisitFuncs{
				Leave: func(p visitor.VisitFuncParams) (string, interface{}) {
					if selectionSet, ok := p.Node.(*ast.SelectionSet); ok && selectionSet != nil {
						parentType, _ := context.ParentType().(Named)
						fieldMap := collectFieldASTsAndDefs(
							context,
							parentType,
							selectionSet,
							nil,
							nil,
						)
						conflicts := findConflicts(fieldMap)
						if len(conflicts) > 0 {
							for _, c := range conflicts {
								responseName := c.Reason.Name
								reason := c.Reason
								reportError(
									context,
									fmt.Sprintf(
										`Fields "%v" conflict because %v.`,
										responseName,
										reasonMessage(reason),
									),
									c.Fields,
								)
							}
							return visitor.ActionNoChange, nil
						}
					}
					return visitor.ActionNoChange, nil
				},
			},
		},
	}
	return &ValidationRuleInstance{
		VisitorOpts: visitorOpts,
	}
}

func getFragmentType(context *ValidationContext, name string) Type {
	frag := context.Fragment(name)
	if frag == nil {
		return nil
	}
	ttype, _ := typeFromAST(*context.Schema(), frag.TypeCondition)
	return ttype
}

func doTypesOverlap(t1 Type, t2 Type) bool {
	if t1 == t2 {
		return true
	}
	if _, ok := t1.(*Object); ok {
		if _, ok := t2.(*Object); ok {
			return false
		}
		if t2, ok := t2.(Abstract); ok {
			for _, ttype := range t2.PossibleTypes() {
				if ttype == t1 {
					return true
				}
			}
			return false
		}
	}
	if t1, ok := t1.(Abstract); ok {
		if _, ok := t2.(*Object); ok {
			for _, ttype := range t1.PossibleTypes() {
				if ttype == t2 {
					return true
				}
			}
			return false
		}
		t1TypeNames := map[string]bool{}
		for _, ttype := range t1.PossibleTypes() {
			t1TypeNames[ttype.Name()] = true
		}
		if t2, ok := t2.(Abstract); ok {
			for _, ttype := range t2.PossibleTypes() {
				if hasT1TypeName, _ := t1TypeNames[ttype.Name()]; hasT1TypeName {
					return true
				}
			}
			return false
		}
	}
	return false
}

/**
 * PossibleFragmentSpreadsRule
 * Possible fragment spread
 *
 * A fragment spread is only valid if the type condition could ever possibly
 * be true: if there is a non-empty intersection of the possible parent types,
 * and possible types which pass the type condition.
 */
func PossibleFragmentSpreadsRule(context *ValidationContext) *ValidationRuleInstance {

	visitorOpts := &visitor.VisitorOptions{
		KindFuncMap: map[string]visitor.NamedVisitFuncs{
			kinds.InlineFragment: visitor.NamedVisitFuncs{
				Kind: func(p visitor.VisitFuncParams) (string, interface{}) {
					if node, ok := p.Node.(*ast.InlineFragment); ok && node != nil {
						fragType := context.Type()
						parentType, _ := context.ParentType().(Type)

						if fragType != nil && parentType != nil && !doTypesOverlap(fragType, parentType) {
							return reportError(
								context,
								fmt.Sprintf(`Fragment cannot be spread here as objects of `+
									`type "%v" can never be of type "%v".`, parentType, fragType),
								[]ast.Node{node},
							)
						}
					}
					return visitor.ActionNoChange, nil
				},
			},
			kinds.FragmentSpread: visitor.NamedVisitFuncs{
				Kind: func(p visitor.VisitFuncParams) (string, interface{}) {
					if node, ok := p.Node.(*ast.FragmentSpread); ok && node != nil {
						fragName := ""
						if node.Name != nil {
							fragName = node.Name.Value
						}
						fragType := getFragmentType(context, fragName)
						parentType, _ := context.ParentType().(Type)
						if fragType != nil && parentType != nil && !doTypesOverlap(fragType, parentType) {
							return reportError(
								context,
								fmt.Sprintf(`Fragment "%v" cannot be spread here as objects of `+
									`type "%v" can never be of type "%v".`, fragName, parentType, fragType),
								[]ast.Node{node},
							)
						}
					}
					return visitor.ActionNoChange, nil
				},
			},
		},
	}
	return &ValidationRuleInstance{
		VisitorOpts: visitorOpts,
	}
}

/**
 * ProvidedNonNullArgumentsRule
 * Provided required arguments
 *
 * A field or directive is only valid if all required (non-null) field arguments
 * have been provided.
 */
func ProvidedNonNullArgumentsRule(context *ValidationContext) *ValidationRuleInstance {

	visitorOpts := &visitor.VisitorOptions{
		KindFuncMap: map[string]visitor.NamedVisitFuncs{
			kinds.Field: visitor.NamedVisitFuncs{
				Leave: func(p visitor.VisitFuncParams) (string, interface{}) {
					// Validate on leave to allow for deeper errors to appear first.
					if fieldAST, ok := p.Node.(*ast.Field); ok && fieldAST != nil {
						fieldDef := context.FieldDef()
						if fieldDef == nil {
							return visitor.ActionSkip, nil
						}

						argASTs := fieldAST.Arguments

						argASTMap := map[string]*ast.Argument{}
						for _, arg := range argASTs {
							name := ""
							if arg.Name != nil {
								name = arg.Name.Value
							}
							argASTMap[name] = arg
						}
						for _, argDef := range fieldDef.Args {
							argAST, _ := argASTMap[argDef.Name()]
							if argAST == nil {
								if argDefType, ok := argDef.Type.(*NonNull); ok {
									fieldName := ""
									if fieldAST.Name != nil {
										fieldName = fieldAST.Name.Value
									}
									reportError(
										context,
										fmt.Sprintf(`Field "%v" argument "%v" of type "%v" `+
											`is required but not provided.`, fieldName, argDef.Name(), argDefType),
										[]ast.Node{fieldAST},
									)
								}
							}
						}
					}
					return visitor.ActionNoChange, nil
				},
			},
			kinds.Directive: visitor.NamedVisitFuncs{
				Kind: func(p visitor.VisitFuncParams) (string, interface{}) {
					// Validate on leave to allow for deeper errors to appear first.

					if directiveAST, ok := p.Node.(*ast.Directive); ok && directiveAST != nil {
						directiveDef := context.Directive()
						if directiveDef == nil {
							return visitor.ActionSkip, nil
						}
						argASTs := directiveAST.Arguments

						argASTMap := map[string]*ast.Argument{}
						for _, arg := range argASTs {
							name := ""
							if arg.Name != nil {
								name = arg.Name.Value
							}
							argASTMap[name] = arg
						}

						for _, argDef := range directiveDef.Args {
							argAST, _ := argASTMap[argDef.Name()]
							if argAST == nil {
								if argDefType, ok := argDef.Type.(*NonNull); ok {
									directiveName := ""
									if directiveAST.Name != nil {
										directiveName = directiveAST.Name.Value
									}
									reportError(
										context,
										fmt.Sprintf(`Directive "@%v" argument "%v" of type `+
											`"%v" is required but not provided.`, directiveName, argDef.Name(), argDefType),
										[]ast.Node{directiveAST},
									)
								}
							}
						}
					}
					return visitor.ActionNoChange, nil
				},
			},
		},
	}
	return &ValidationRuleInstance{
		VisitorOpts: visitorOpts,
	}
}

/**
 * ScalarLeafsRule
 * Scalar leafs
 *
 * A GraphQL document is valid only if all leaf fields (fields without
 * sub selections) are of scalar or enum types.
 */
func ScalarLeafsRule(context *ValidationContext) *ValidationRuleInstance {

	visitorOpts := &visitor.VisitorOptions{
		KindFuncMap: map[string]visitor.NamedVisitFuncs{
			kinds.Field: visitor.NamedVisitFuncs{
				Kind: func(p visitor.VisitFuncParams) (string, interface{}) {
					if node, ok := p.Node.(*ast.Field); ok && node != nil {
						nodeName := ""
						if node.Name != nil {
							nodeName = node.Name.Value
						}
						ttype := context.Type()
						if ttype != nil {
							if IsLeafType(ttype) {
								if node.SelectionSet != nil {
									return reportError(
										context,
										fmt.Sprintf(`Field "%v" of type "%v" must not have a sub selection.`, nodeName, ttype),
										[]ast.Node{node.SelectionSet},
									)
								}
							} else if node.SelectionSet == nil {
								return reportError(
									context,
									fmt.Sprintf(`Field "%v" of type "%v" must have a sub selection.`, nodeName, ttype),
									[]ast.Node{node},
								)
							}
						}
					}
					return visitor.ActionNoChange, nil
				},
			},
		},
	}
	return &ValidationRuleInstance{
		VisitorOpts: visitorOpts,
	}
}

/**
 * UniqueArgumentNamesRule
 * Unique argument names
 *
 * A GraphQL field or directive is only valid if all supplied arguments are
 * uniquely named.
 */
func UniqueArgumentNamesRule(context *ValidationContext) *ValidationRuleInstance {
	knownArgNames := map[string]*ast.Name{}

	visitorOpts := &visitor.VisitorOptions{
		KindFuncMap: map[string]visitor.NamedVisitFuncs{
			kinds.Field: visitor.NamedVisitFuncs{
				Kind: func(p visitor.VisitFuncParams) (string, interface{}) {
					knownArgNames = map[string]*ast.Name{}
					return visitor.ActionNoChange, nil
				},
			},
			kinds.Directive: visitor.NamedVisitFuncs{
				Kind: func(p visitor.VisitFuncParams) (string, interface{}) {
					knownArgNames = map[string]*ast.Name{}
					return visitor.ActionNoChange, nil
				},
			},
			kinds.Argument: visitor.NamedVisitFuncs{
				Kind: func(p visitor.VisitFuncParams) (string, interface{}) {
					if node, ok := p.Node.(*ast.Argument); ok {
						argName := ""
						if node.Name != nil {
							argName = node.Name.Value
						}
						if nameAST, ok := knownArgNames[argName]; ok {
							return reportError(
								context,
								fmt.Sprintf(`There can be only one argument named "%v".`, argName),
								[]ast.Node{nameAST, node.Name},
							)
						}
						knownArgNames[argName] = node.Name
					}
					return visitor.ActionNoChange, nil
				},
			},
		},
	}
	return &ValidationRuleInstance{
		VisitorOpts: visitorOpts,
	}
}

/**
 * UniqueFragmentNamesRule
 * Unique fragment names
 *
 * A GraphQL document is only valid if all defined fragments have unique names.
 */
func UniqueFragmentNamesRule(context *ValidationContext) *ValidationRuleInstance {
	knownFragmentNames := map[string]*ast.Name{}

	visitorOpts := &visitor.VisitorOptions{
		KindFuncMap: map[string]visitor.NamedVisitFuncs{
			kinds.FragmentDefinition: visitor.NamedVisitFuncs{
				Kind: func(p visitor.VisitFuncParams) (string, interface{}) {
					if node, ok := p.Node.(*ast.FragmentDefinition); ok && node != nil {
						fragmentName := ""
						if node.Name != nil {
							fragmentName = node.Name.Value
						}
						if nameAST, ok := knownFragmentNames[fragmentName]; ok {
							return reportError(
								context,
								fmt.Sprintf(`There can only be one fragment named "%v".`, fragmentName),
								[]ast.Node{nameAST, node.Name},
							)
						}
						knownFragmentNames[fragmentName] = node.Name
					}
					return visitor.ActionNoChange, nil
				},
			},
		},
	}
	return &ValidationRuleInstance{
		VisitorOpts: visitorOpts,
	}
}

/**
 * UniqueInputFieldNamesRule
 *
 * A GraphQL input object value is only valid if all supplied fields are
 * uniquely named.
 */
func UniqueInputFieldNamesRule(context *ValidationContext) *ValidationRuleInstance {
	knownNameStack := []map[string]*ast.Name{}
	knownNames := map[string]*ast.Name{}

	visitorOpts := &visitor.VisitorOptions{
		KindFuncMap: map[string]visitor.NamedVisitFuncs{
			kinds.ObjectValue: visitor.NamedVisitFuncs{
				Enter: func(p visitor.VisitFuncParams) (string, interface{}) {
					knownNameStack = append(knownNameStack, knownNames)
					knownNames = map[string]*ast.Name{}
					return visitor.ActionNoChange, nil
				},
				Leave: func(p visitor.VisitFuncParams) (string, interface{}) {
					// pop
					knownNames, knownNameStack = knownNameStack[len(knownNameStack)-1], knownNameStack[:len(knownNameStack)-1]
					return visitor.ActionNoChange, nil
				},
			},
			kinds.ObjectField: visitor.NamedVisitFuncs{
				Kind: func(p visitor.VisitFuncParams) (string, interface{}) {
					var action = visitor.ActionNoChange
					var result interface{}
					if node, ok := p.Node.(*ast.ObjectField); ok {
						fieldName := ""
						if node.Name != nil {
							fieldName = node.Name.Value
						}
						if knownNameAST, ok := knownNames[fieldName]; ok {
							return reportError(
								context,
								fmt.Sprintf(`There can be only one input field named "%v".`, fieldName),
								[]ast.Node{knownNameAST, node.Name},
							)
						} else {
							knownNames[fieldName] = node.Name
						}

					}
					return action, result
				},
			},
		},
	}
	return &ValidationRuleInstance{
		VisitorOpts: visitorOpts,
	}
}

/**
 * UniqueOperationNamesRule
 * Unique operation names
 *
 * A GraphQL document is only valid if all defined operations have unique names.
 */
func UniqueOperationNamesRule(context *ValidationContext) *ValidationRuleInstance {
	knownOperationNames := map[string]*ast.Name{}

	visitorOpts := &visitor.VisitorOptions{
		KindFuncMap: map[string]visitor.NamedVisitFuncs{
			kinds.OperationDefinition: visitor.NamedVisitFuncs{
				Kind: func(p visitor.VisitFuncParams) (string, interface{}) {
					if node, ok := p.Node.(*ast.OperationDefinition); ok && node != nil {
						operationName := ""
						if node.Name != nil {
							operationName = node.Name.Value
						}
						if nameAST, ok := knownOperationNames[operationName]; ok {
							return reportError(
								context,
								fmt.Sprintf(`There can only be one operation named "%v".`, operationName),
								[]ast.Node{nameAST, node.Name},
							)
						}
						knownOperationNames[operationName] = node.Name
					}
					return visitor.ActionNoChange, nil
				},
			},
		},
	}
	return &ValidationRuleInstance{
		VisitorOpts: visitorOpts,
	}
}

/**
 * Unique variable names
 *
 * A GraphQL operation is only valid if all its variables are uniquely named.
 */
func UniqueVariableNamesRule(context *ValidationContext) *ValidationRuleInstance {
	knownVariableNames := map[string]*ast.Name{}

	visitorOpts := &visitor.VisitorOptions{
		KindFuncMap: map[string]visitor.NamedVisitFuncs{
			kinds.OperationDefinition: visitor.NamedVisitFuncs{
				Kind: func(p visitor.VisitFuncParams) (string, interface{}) {
					if node, ok := p.Node.(*ast.OperationDefinition); ok && node != nil {
						knownVariableNames = map[string]*ast.Name{}
					}
					return visitor.ActionNoChange, nil
				},
			},
			kinds.VariableDefinition: visitor.NamedVisitFuncs{
				Kind: func(p visitor.VisitFuncParams) (string, interface{}) {
					if node, ok := p.Node.(*ast.VariableDefinition); ok && node != nil {
						variableName := ""
						var variableNameAST *ast.Name
						if node.Variable != nil && node.Variable.Name != nil {
							variableNameAST = node.Variable.Name
							variableName = node.Variable.Name.Value
						}
						if nameAST, ok := knownVariableNames[variableName]; ok {
							return reportError(
								context,
								fmt.Sprintf(`There can only be one variable named "%v".`, variableName),
								[]ast.Node{nameAST, variableNameAST},
							)
						}
						if variableNameAST != nil {
							knownVariableNames[variableName] = variableNameAST
						}
					}
					return visitor.ActionNoChange, nil
				},
			},
		},
	}
	return &ValidationRuleInstance{
		VisitorOpts: visitorOpts,
	}
}

/**
 * VariablesAreInputTypesRule
 * Variables are input types
 *
 * A GraphQL operation is only valid if all the variables it defines are of
 * input types (scalar, enum, or input object).
 */
func VariablesAreInputTypesRule(context *ValidationContext) *ValidationRuleInstance {

	visitorOpts := &visitor.VisitorOptions{
		KindFuncMap: map[string]visitor.NamedVisitFuncs{
			kinds.VariableDefinition: visitor.NamedVisitFuncs{
				Kind: func(p visitor.VisitFuncParams) (string, interface{}) {
					if node, ok := p.Node.(*ast.VariableDefinition); ok && node != nil {
						ttype, _ := typeFromAST(*context.Schema(), node.Type)

						// If the variable type is not an input type, return an error.
						if ttype != nil && !IsInputType(ttype) {
							variableName := ""
							if node.Variable != nil && node.Variable.Name != nil {
								variableName = node.Variable.Name.Value
							}
							return reportError(
								context,
								fmt.Sprintf(`Variable "$%v" cannot be non-input type "%v".`,
									variableName, printer.Print(node.Type)),
								[]ast.Node{node.Type},
							)
						}
					}
					return visitor.ActionNoChange, nil
				},
			},
		},
	}
	return &ValidationRuleInstance{
		VisitorOpts: visitorOpts,
	}
}

// If a variable definition has a default value, it's effectively non-null.
func effectiveType(varType Type, varDef *ast.VariableDefinition) Type {
	if varDef.DefaultValue == nil {
		return varType
	}
	if _, ok := varType.(*NonNull); ok {
		return varType
	}
	return NewNonNull(varType)
}

/**
 * VariablesInAllowedPositionRule
 * Variables passed to field arguments conform to type
 */
func VariablesInAllowedPositionRule(context *ValidationContext) *ValidationRuleInstance {

	varDefMap := map[string]*ast.VariableDefinition{}

	visitorOpts := &visitor.VisitorOptions{
		KindFuncMap: map[string]visitor.NamedVisitFuncs{
			kinds.OperationDefinition: visitor.NamedVisitFuncs{
				Enter: func(p visitor.VisitFuncParams) (string, interface{}) {
					varDefMap = map[string]*ast.VariableDefinition{}
					return visitor.ActionNoChange, nil
				},
				Leave: func(p visitor.VisitFuncParams) (string, interface{}) {
					if operation, ok := p.Node.(*ast.OperationDefinition); ok {

						usages := context.RecursiveVariableUsages(operation)
						for _, usage := range usages {
							varName := ""
							if usage != nil && usage.Node != nil && usage.Node.Name != nil {
								varName = usage.Node.Name.Value
							}
							var varType Type
							varDef, ok := varDefMap[varName]
							if ok {
								// A var type is allowed if it is the same or more strict (e.g. is
								// a subtype of) than the expected type. It can be more strict if
								// the variable type is non-null when the expected type is nullable.
								// If both are list types, the variable item type can be more strict
								// than the expected item type (contravariant).
								var err error
								varType, err = typeFromAST(*context.Schema(), varDef.Type)
								if err != nil {
									varType = nil
								}
							}
							if varType != nil &&
								usage.Type != nil &&
								!isTypeSubTypeOf(effectiveType(varType, varDef), usage.Type) {
								reportError(
									context,
									fmt.Sprintf(`Variable "$%v" of type "%v" used in position `+
										`expecting type "%v".`, varName, varType, usage.Type),
									[]ast.Node{usage.Node},
								)
							}
						}

					}
					return visitor.ActionNoChange, nil
				},
			},
			kinds.VariableDefinition: visitor.NamedVisitFuncs{
				Kind: func(p visitor.VisitFuncParams) (string, interface{}) {
					if varDefAST, ok := p.Node.(*ast.VariableDefinition); ok {
						defName := ""
						if varDefAST.Variable != nil && varDefAST.Variable.Name != nil {
							defName = varDefAST.Variable.Name.Value
						}
						if defName != "" {
							varDefMap[defName] = varDefAST
						}
					}
					return visitor.ActionNoChange, nil
				},
			},
		},
	}
	return &ValidationRuleInstance{
		VisitSpreadFragments: true,
		VisitorOpts:          visitorOpts,
	}
}

/**
 * Utility for validators which determines if a value literal AST is valid given
 * an input type.
 *
 * Note that this only validates literal values, variables are assumed to
 * provide values of the correct type.
 */
func isValidLiteralValue(ttype Input, valueAST ast.Value) (bool, []string) {
	// A value must be provided if the type is non-null.
	if ttype, ok := ttype.(*NonNull); ok {
		if valueAST == nil {
			if ttype.OfType.Name() != "" {
				return false, []string{fmt.Sprintf(`Expected "%v!", found null.`, ttype.OfType.Name())}
			}
			return false, []string{"Expected non-null value, found null."}
		}
		ofType, _ := ttype.OfType.(Input)
		return isValidLiteralValue(ofType, valueAST)
	}

	if valueAST == nil {
		return true, nil
	}

	// This function only tests literals, and assumes variables will provide
	// values of the correct type.
	if valueAST.GetKind() == kinds.Variable {
		return true, nil
	}

	// Lists accept a non-list value as a list of one.
	if ttype, ok := ttype.(*List); ok {
		itemType, _ := ttype.OfType.(Input)
		if valueAST, ok := valueAST.(*ast.ListValue); ok {
			messagesReduce := []string{}
			for _, value := range valueAST.Values {
				_, messages := isValidLiteralValue(itemType, value)
				for idx, message := range messages {
					messagesReduce = append(messagesReduce, fmt.Sprintf(`In element #%v: %v`, idx+1, message))
				}
			}
			return (len(messagesReduce) == 0), messagesReduce
		}
		return isValidLiteralValue(itemType, valueAST)

	}

	// Input objects check each defined field and look for undefined fields.
	if ttype, ok := ttype.(*InputObject); ok {
		valueAST, ok := valueAST.(*ast.ObjectValue)
		if !ok {
			return false, []string{fmt.Sprintf(`Expected "%v", found not an object.`, ttype.Name())}
		}
		fields := ttype.Fields()
		messagesReduce := []string{}

		// Ensure every provided field is defined.
		fieldASTs := valueAST.Fields
		fieldASTMap := map[string]*ast.ObjectField{}
		for _, fieldAST := range fieldASTs {
			fieldASTName := ""
			if fieldAST.Name != nil {
				fieldASTName = fieldAST.Name.Value
			}

			fieldASTMap[fieldASTName] = fieldAST

			field, ok := fields[fieldASTName]
			if !ok || field == nil {
				messagesReduce = append(messagesReduce, fmt.Sprintf(`In field "%v": Unknown field.`, fieldASTName))
			}
		}
		// Ensure every defined field is valid.
		for fieldName, field := range fields {
			fieldAST, _ := fieldASTMap[fieldName]
			var fieldASTValue ast.Value
			if fieldAST != nil {
				fieldASTValue = fieldAST.Value
			}
			if isValid, messages := isValidLiteralValue(field.Type, fieldASTValue); !isValid {
				for _, message := range messages {
					messagesReduce = append(messagesReduce, fmt.Sprintf("In field \"%v\": %v", fieldName, message))
				}
			}
		}
		return (len(messagesReduce) == 0), messagesReduce
	}

	if ttype, ok := ttype.(*Scalar); ok {
		if isNullish(ttype.ParseLiteral(valueAST)) {
			return false, []string{fmt.Sprintf(`Expected type "%v", found %v.`, ttype.Name(), printer.Print(valueAST))}
		}
	}
	if ttype, ok := ttype.(*Enum); ok {
		if isNullish(ttype.ParseLiteral(valueAST)) {
			return false, []string{fmt.Sprintf(`Expected type "%v", found %v.`, ttype.Name(), printer.Print(valueAST))}
		}
	}

	return true, nil
}

/**
 * Given an operation or fragment AST node, gather all the
 * named spreads defined within the scope of the fragment
 * or operation
 */
func gatherSpreads(node ast.Node) (spreadNodes []*ast.FragmentSpread) {
	visitorOpts := &visitor.VisitorOptions{
		KindFuncMap: map[string]visitor.NamedVisitFuncs{
			kinds.FragmentSpread: visitor.NamedVisitFuncs{
				Kind: func(p visitor.VisitFuncParams) (string, interface{}) {
					if node, ok := p.Node.(*ast.FragmentSpread); ok && node != nil {
						spreadNodes = append(spreadNodes, node)
					}
					return visitor.ActionNoChange, nil
				},
			},
		},
	}
	visitor.Visit(node, visitorOpts, nil)
	return spreadNodes
}
