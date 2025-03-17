package hcl

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/docker/docker-language-server/internal/bake/hcl/parser"
	"github.com/docker/docker-language-server/internal/pkg/document"
	"github.com/docker/docker-language-server/internal/tliron/glsp/protocol"
	"github.com/hashicorp/hcl-lang/decoder"
	"github.com/hashicorp/hcl-lang/lang"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"go.lsp.dev/uri"
)

func Completion(ctx context.Context, params *protocol.CompletionParams, manager *document.Manager, document document.BakeHCLDocument) (*protocol.CompletionList, error) {
	filename := string(params.TextDocument.URI)

	hclPos := parser.ConvertToHCLPosition(string(document.Input()), int(params.Position.Line), int(params.Position.Character))
	candidates, err := document.Decoder().CompletionAtPos(ctx, filename, hclPos)
	if err != nil {
		var rangeErr *decoder.PosOutOfRangeError
		if errors.As(err, &rangeErr) {
			return &protocol.CompletionList{Items: []protocol.CompletionItem{}}, nil
		}
		var positionalError *decoder.PositionalError
		if !errors.As(err, &positionalError) {
			return nil, fmt.Errorf("textDocument/completion encountered an error: %w", err)
		}
	}
	body, ok := document.File().Body.(*hclsyntax.Body)
	if !ok {
		return nil, errors.New("unrecognized body in HCL document")
	}

	dockerfilePath, err := ParseReferencedDockerfile(uri.URI(params.TextDocument.URI), document, int(params.Position.Line)+1, int(params.Position.Character)+1)
	if err != nil {
		return nil, fmt.Errorf("textDocument/completion encountered an error: %w", err)
	}
	for _, b := range body.Blocks {
		if isInsideBodyRangeLines(b.Body, int(params.Position.Line)+1) {
			if dockerfilePath != "" {
				attributes := b.Body.Attributes
				if attribute, ok := attributes["inherits"]; ok && isInsideRange(attribute.Expr.Range(), params.Position) {
					if tupleConsExpr, ok := attribute.Expr.(*hclsyntax.TupleConsExpr); ok {
						if len(tupleConsExpr.Exprs) == 0 {
							return createTargetBlockCompletionItems(body.Blocks, true), nil
						}

						for _, e := range tupleConsExpr.Exprs {
							if templateExpr, ok := e.(*hclsyntax.TemplateExpr); ok {
								if templateExpr.IsStringLiteral() {
									return createTargetBlockCompletionItems(body.Blocks, false), nil
								}
							}
						}
					}
				}

				_, nodes := OpenDockerfile(ctx, manager, dockerfilePath)
				if nodes != nil {
					if attribute, ok := attributes["target"]; ok && isInsideRange(attribute.Expr.Range(), params.Position) {
						if _, ok := attributes["dockerfile-inline"]; ok {
							return &protocol.CompletionList{Items: []protocol.CompletionItem{}}, nil
						}

						list := &protocol.CompletionList{}
						for _, child := range nodes {
							if strings.EqualFold(child.Value, "FROM") && child.Next != nil && child.Next.Next != nil && child.Next.Next.Next != nil {
								item := protocol.CompletionItem{
									Label: child.Next.Next.Next.Value,
								}
								list.Items = append(list.Items, item)
							}
						}
						return list, nil
					}

					if attribute, ok := attributes["args"]; ok {
						if expr, ok := attribute.Expr.(*hclsyntax.ObjectConsExpr); ok {
							for _, item := range expr.Items {
								if isInsideRange(item.KeyExpr.Range(), params.Position) {
									list := &protocol.CompletionList{}
									for _, child := range nodes {
										if child.Value == "ARG" && child.Next != nil {
											node := child.Next
											for node != nil {
												value := node.Value
												idx := strings.Index(value, "=")
												if idx != -1 {
													value = value[0:idx]
												}
												item := protocol.CompletionItem{
													Label: value,
												}
												list.Items = append(list.Items, item)
												node = node.Next
											}
										}
									}
									return list, nil
								}
							}
						}
					}
					break
				}
			}
		}
	}

	list := &protocol.CompletionList{Items: []protocol.CompletionItem{}}
	for _, c := range candidates.List {
		format := protocol.InsertTextFormatSnippet
		item := protocol.CompletionItem{
			Detail:           &c.Detail,
			Label:            c.Label,
			InsertTextFormat: &format,
			TextEdit: &protocol.TextEdit{
				Range: protocol.Range{
					Start: protocol.Position{Line: uint32(c.TextEdit.Range.Start.Line - 1), Character: uint32(c.TextEdit.Range.Start.Column - 1)},
					End:   protocol.Position{Line: uint32(c.TextEdit.Range.End.Line - 1), Character: uint32(c.TextEdit.Range.End.Column - 1)},
				},
				NewText: c.TextEdit.Snippet,
			},
		}

		if c.SortText != "" {
			item.SortText = &c.SortText
		}
		var kind protocol.CompletionItemKind
		switch c.Kind {
		case lang.BlockCandidateKind:
			kind = protocol.CompletionItemKindClass
			item.Kind = &kind
		case lang.AttributeCandidateKind:
			kind = protocol.CompletionItemKindProperty
			item.Kind = &kind
		}
		list.Items = append(list.Items, item)
	}
	return list, nil
}

func isInsideBodyRangeLines(body *hclsyntax.Body, line int) bool {
	return body.Range().Start.Line <= line && line <= body.Range().End.Line
}

func isInsideRange(rng hcl.Range, position protocol.Position) bool {
	positionLine := int(position.Line + 1)
	positionCharacter := int(position.Character + 1)
	if rng.Start.Line < positionLine {
		if positionLine < rng.End.Line {
			return true
		}
		return positionLine == rng.End.Line && positionCharacter < rng.End.Column
	} else if rng.Start.Line == positionLine {
		if positionLine < rng.End.Line {
			return rng.Start.Column <= positionCharacter
		}
		return rng.Start.Column <= positionCharacter && positionCharacter <= rng.End.Column
	}
	return false
}

func createTargetBlockCompletionItems(blocks hclsyntax.Blocks, quoted bool) *protocol.CompletionList {
	list := &protocol.CompletionList{Items: []protocol.CompletionItem{}}
	for _, block := range blocks {
		if block.Type == "target" && len(block.Labels) > 0 {
			item := protocol.CompletionItem{
				Label: block.Labels[0],
			}

			if quoted {
				insertText := fmt.Sprintf("\"%v\"", block.Labels[0])
				item.InsertText = &insertText
			}
			list.Items = append(list.Items, item)
		}
	}
	return list
}
