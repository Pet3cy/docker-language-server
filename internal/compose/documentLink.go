package compose

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"strings"

	"github.com/docker/docker-language-server/internal/pkg/document"
	"github.com/docker/docker-language-server/internal/tliron/glsp/protocol"
	"github.com/docker/docker-language-server/internal/types"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/token"
)

func createRange(t *token.Token, length int) protocol.Range {
	offset := 0
	if t.Type == token.DoubleQuoteType {
		offset = 1
	}
	return protocol.Range{
		Start: protocol.Position{
			Line:      protocol.UInteger(t.Position.Line - 1),
			Character: protocol.UInteger(t.Position.Column + offset - 1),
		},
		End: protocol.Position{
			Line:      protocol.UInteger(t.Position.Line - 1),
			Character: protocol.UInteger(t.Position.Column + offset + length - 1),
		},
	}
}

func createLink(folderAbsolutePath string, wslDollarSign bool, node *token.Token) *protocol.DocumentLink {
	file := node.Value
	if wslDollarSign {
		return &protocol.DocumentLink{
			Range:   createRange(node, len(file)),
			Target:  types.CreateStringPointer("file://wsl%24" + path.Join(strings.ReplaceAll(folderAbsolutePath, "\\", "/"), file)),
			Tooltip: types.CreateStringPointer("\\\\wsl%24" + strings.ReplaceAll(path.Join(folderAbsolutePath, file), "/", "\\")),
		}
	}
	abs := filepath.ToSlash(filepath.Join(folderAbsolutePath, file))
	return &protocol.DocumentLink{
		Range:   createRange(node, len(file)),
		Target:  types.CreateStringPointer(protocol.URI(fmt.Sprintf("file:///%v", strings.TrimPrefix(abs, "/")))),
		Tooltip: types.CreateStringPointer(filepath.FromSlash(abs)),
	}
}

func createFileLink(folderAbsolutePath string, wslDollarSign bool, serviceNode *ast.MappingValueNode) *protocol.DocumentLink {
	attributeValue := stringNode(serviceNode.Value)
	if attributeValue != nil {
		return createLink(folderAbsolutePath, wslDollarSign, attributeValue.GetToken())
	}
	return nil
}

func stringNode(value ast.Node) *ast.StringNode {
	if s, ok := resolveAnchor(value).(*ast.StringNode); ok {
		return s
	}
	return nil
}

func createdNestedLink(folderAbsolutePath string, wslDollarSign bool, serviceNode *ast.MappingValueNode, parent, child string) *protocol.DocumentLink {
	if resolveAnchor(serviceNode.Key).GetToken().Value == parent {
		if mappingNode, ok := resolveAnchor(serviceNode.Value).(*ast.MappingNode); ok {
			for _, buildAttribute := range mappingNode.Values {
				if resolveAnchor(buildAttribute.Key).GetToken().Value == child {
					return createFileLink(folderAbsolutePath, wslDollarSign, buildAttribute)
				}
			}
		}
	}
	return nil
}

func createImageLink(serviceNode *ast.MappingValueNode) *protocol.DocumentLink {
	if resolveAnchor(serviceNode.Key).GetToken().Value == "image" {
		service := stringNode(serviceNode.Value)
		if service != nil {
			linkedText, link := extractImageLink(service.Value)
			if linkedText != "" {
				return &protocol.DocumentLink{
					Range:   createRange(service.GetToken(), len(linkedText)),
					Target:  types.CreateStringPointer(link),
					Tooltip: types.CreateStringPointer(link),
				}
			}
		}
	}
	return nil
}

func createLabelFileLink(folderAbsolutePath string, wslDollarSign bool, serviceNode *ast.MappingValueNode) []protocol.DocumentLink {
	if resolveAnchor(serviceNode.Key).GetToken().Value == "label_file" {
		if sequence, ok := resolveAnchor(serviceNode.Value).(*ast.SequenceNode); ok {
			links := []protocol.DocumentLink{}
			for _, node := range sequence.Values {
				if s, ok := resolveAnchor(node).(*ast.StringNode); ok {
					links = append(links, *createLink(folderAbsolutePath, wslDollarSign, s.GetToken()))
				}
			}
			return links
		}

		link := createFileLink(folderAbsolutePath, wslDollarSign, serviceNode)
		if link != nil {
			return []protocol.DocumentLink{*link}
		}
	}
	return nil
}

func createObjectFileLink(folderAbsolutePath string, wslDollarSign bool, serviceNode *ast.MappingValueNode) *protocol.DocumentLink {
	if resolveAnchor(serviceNode.Key).GetToken().Value == "file" {
		return createFileLink(folderAbsolutePath, wslDollarSign, serviceNode)
	}
	return nil
}

func createModelLink(serviceNode *ast.MappingValueNode) *protocol.DocumentLink {
	if resolveAnchor(serviceNode.Key).GetToken().Value == "model" {
		service := stringNode(serviceNode.Value)
		if service != nil {
			linkedText, link := extractModelLink(service.Value)
			if linkedText != "" {
				return &protocol.DocumentLink{
					Range:   createRange(service.GetToken(), len(linkedText)),
					Target:  types.CreateStringPointer(link),
					Tooltip: types.CreateStringPointer(link),
				}
			}
		}
	}
	return nil
}

func includedFiles(nodes []ast.Node) []*token.Token {
	tokens := []*token.Token{}
	for _, entry := range nodes {
		if mappingNode, ok := resolveAnchor(entry).(*ast.MappingNode); ok {
			for _, value := range mappingNode.Values {
				if resolveAnchor(value.Key).GetToken().Value == "path" {
					if paths, ok := resolveAnchor(value.Value).(*ast.SequenceNode); ok {
						// include:
						//   - path:
						//     - ../commons/compose.yaml
						//     - ./commons-override.yaml
						for _, path := range paths.Values {
							tokens = append(tokens, resolveAnchor(path).GetToken())
						}
					} else {
						// include:
						// - path: ../commons/compose.yaml
						//   project_directory: ..
						//   env_file: ../another/.env
						tokens = append(tokens, resolveAnchor(value.Value).GetToken())
					}
				}
			}
		} else {
			// include:
			//   - abc.yml
			//   - def.yml
			stringNode := stringNode(entry)
			if stringNode != nil {
				tokens = append(tokens, stringNode.GetToken())
			}

		}
	}
	return tokens
}

func scanForLinks(folderAbsolutePath string, wslDollarSign bool, n *ast.MappingValueNode) []protocol.DocumentLink {
	if s, ok := resolveAnchor(n.Key).(*ast.StringNode); ok {
		links := []protocol.DocumentLink{}
		switch s.Value {
		case "include":
			if sequence, ok := resolveAnchor(n.Value).(*ast.SequenceNode); ok {
				for _, token := range includedFiles(sequence.Values) {
					link := createLink(folderAbsolutePath, wslDollarSign, token)
					if link != nil {
						links = append(links, *link)
					}
				}
			}
		case "services":
			if mappingNode, ok := resolveAnchor(n.Value).(*ast.MappingNode); ok {
				for _, node := range mappingNode.Values {
					if serviceAttributes, ok := resolveAnchor(node.Value).(*ast.MappingNode); ok {
						for _, serviceAttribute := range serviceAttributes.Values {
							link := createImageLink(serviceAttribute)
							if link != nil {
								links = append(links, *link)
							}

							link = createdNestedLink(folderAbsolutePath, wslDollarSign, serviceAttribute, "build", "dockerfile")
							if link != nil {
								links = append(links, *link)
							}

							link = createdNestedLink(folderAbsolutePath, wslDollarSign, serviceAttribute, "credential_spec", "file")
							if link != nil {
								links = append(links, *link)
							}

							link = createdNestedLink(folderAbsolutePath, wslDollarSign, serviceAttribute, "extends", "file")
							if link != nil {
								links = append(links, *link)
							}

							labelFileLinks := createLabelFileLink(folderAbsolutePath, wslDollarSign, serviceAttribute)
							links = append(links, labelFileLinks...)
						}
					}
				}
			}
		case "configs":
			if mappingNode, ok := resolveAnchor(n.Value).(*ast.MappingNode); ok {
				for _, node := range mappingNode.Values {
					if configAttributes, ok := resolveAnchor(node.Value).(*ast.MappingNode); ok {
						for _, configAttribute := range configAttributes.Values {
							link := createObjectFileLink(folderAbsolutePath, wslDollarSign, configAttribute)
							if link != nil {
								links = append(links, *link)
							}
						}
					}
				}
			}
		case "secrets":
			if mappingNode, ok := resolveAnchor(n.Value).(*ast.MappingNode); ok {
				for _, node := range mappingNode.Values {
					if configAttributes, ok := resolveAnchor(node.Value).(*ast.MappingNode); ok {
						for _, configAttribute := range configAttributes.Values {
							link := createObjectFileLink(folderAbsolutePath, wslDollarSign, configAttribute)
							if link != nil {
								links = append(links, *link)
							}
						}
					}
				}
			}
		case "models":
			if mappingNode, ok := resolveAnchor(n.Value).(*ast.MappingNode); ok {
				for _, node := range mappingNode.Values {
					if serviceAttributes, ok := resolveAnchor(node.Value).(*ast.MappingNode); ok {
						for _, serviceAttribute := range serviceAttributes.Values {
							link := createModelLink(serviceAttribute)
							if link != nil {
								links = append(links, *link)
							}
						}
					}
				}
			}
		}
		return links
	}
	return nil
}

func documentFolder(documentURI protocol.URI) (string, bool, error) {
	url, err := url.Parse(string(documentURI))
	if err != nil {
		if strings.HasPrefix(documentURI, "file://wsl%24/") {
			path := documentURI[len("file://wsl%24"):]
			idx := strings.LastIndex(path, "/")
			return path[0 : idx+1], true, nil
		}
		return "", false, fmt.Errorf("LSP client sent invalid URI: %v", string(documentURI))
	}
	folder, err := types.AbsoluteFolder(url)
	return folder, false, err
}

func DocumentLink(ctx context.Context, documentURI protocol.URI, doc document.ComposeDocument) ([]protocol.DocumentLink, error) {
	abs, wslDollarSign, err := documentFolder(documentURI)
	if err != nil {
		return nil, err
	}

	file := doc.File()
	if file == nil || len(file.Docs) == 0 {
		return nil, nil
	}

	links := []protocol.DocumentLink{}
	for _, documentNode := range file.Docs {
		if mappingNode, ok := documentNode.Body.(*ast.MappingNode); ok {
			for _, node := range mappingNode.Values {
				links = append(links, scanForLinks(abs, wslDollarSign, node)...)
			}
		}
	}
	return links, nil
}

func extractNonDockerHubImageLink(nodeValue, prefix, uriPrefix string, startIndex uint) (string, string) {
	if len(nodeValue) <= len(prefix)+1 {
		return "", ""
	}
	idx := strings.LastIndex(nodeValue, ":")
	lastSlashIdx := strings.LastIndex(nodeValue, "/")
	if (idx != -1 && lastSlashIdx > idx) || strings.Index(nodeValue, "/") == lastSlashIdx {
		return "", ""
	}
	if idx == -1 {
		return nodeValue, fmt.Sprintf("%v%v", uriPrefix, nodeValue[startIndex:])
	}
	return nodeValue[0:idx], fmt.Sprintf("%v%v", uriPrefix, nodeValue[startIndex:idx])
}

func extractImageLink(nodeValue string) (string, string) {
	if strings.HasPrefix(nodeValue, "ghcr.io") {
		return extractNonDockerHubImageLink(nodeValue, "ghcr.io", "https://", 0)
	}

	if strings.HasPrefix(nodeValue, "mcr.microsoft.com") {
		if len(nodeValue) <= 18 {
			return "", ""
		}
		idx := strings.LastIndex(nodeValue, ":")
		if idx == 17 {
			return "", ""
		}
		lastSlashIdx := strings.LastIndex(nodeValue, "/")
		if lastSlashIdx == idx-1 || (idx != -1 && lastSlashIdx > idx) {
			return "", ""
		}
		if idx == -1 {
			return nodeValue, fmt.Sprintf("https://mcr.microsoft.com/artifact/mar/%v", nodeValue[18:])
		}
		return nodeValue[0:idx], fmt.Sprintf("https://mcr.microsoft.com/artifact/mar/%v", nodeValue[18:idx])
	}

	if strings.HasPrefix(nodeValue, "quay.io") {
		return extractNonDockerHubImageLink(nodeValue, "quay.io", "https://quay.io/repository/", 8)
	}

	idx := strings.LastIndex(nodeValue, ":")
	if idx == -1 {
		idx := strings.Index(nodeValue, "/")
		if idx == -1 {
			return nodeValue, fmt.Sprintf("https://hub.docker.com/_/%v", nodeValue)
		}
		return nodeValue, fmt.Sprintf("https://hub.docker.com/r/%v", nodeValue)
	}

	slashIndex := strings.Index(nodeValue, "/")
	if slashIndex == -1 {
		return nodeValue[0:idx], fmt.Sprintf("https://hub.docker.com/_/%v", nodeValue[0:idx])
	}
	return nodeValue[0:idx], fmt.Sprintf("https://hub.docker.com/r/%v", nodeValue[0:idx])
}

func extractModelLink(nodeValue string) (string, string) {
	if strings.HasPrefix(nodeValue, "hf.co") {
		if len(nodeValue) <= 6 {
			return "", ""
		}
		idx := strings.LastIndex(nodeValue, ":")
		if idx == -1 {
			return nodeValue, fmt.Sprintf("https://%v", nodeValue)
		}
		return nodeValue[0:idx], fmt.Sprintf("https://%v", nodeValue[0:idx])
	}

	idx := strings.LastIndex(nodeValue, ":")
	if idx == -1 {
		idx := strings.Index(nodeValue, "/")
		if idx == -1 {
			return nodeValue, fmt.Sprintf("https://hub.docker.com/_/%v", nodeValue)
		}
		return nodeValue, fmt.Sprintf("https://hub.docker.com/r/%v", nodeValue)
	}

	slashIndex := strings.Index(nodeValue, "/")
	if slashIndex == -1 {
		return nodeValue[0:idx], fmt.Sprintf("https://hub.docker.com/_/%v", nodeValue[0:idx])
	}
	return nodeValue[0:idx], fmt.Sprintf("https://hub.docker.com/r/%v", nodeValue[0:idx])
}
