package semcomentario

import (
	"bufio"
	"bytes"
	"fmt"
	"go/scanner"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

var diretoriosIgnorados = map[string]bool{
	".git":         true,
	".claude":      true,
	"node_modules": true,
	"dist":         true,
}

var extensoesDeHash = map[string]bool{
	".sh":      true,
	".ps1":     true,
	".yml":     true,
	".yaml":    true,
	".conf":    true,
	".service": true,
	".toml":    true,
}

var nomesDeHash = map[string]bool{
	"Makefile":      true,
	"Dockerfile":    true,
	".gitignore":    true,
	".dockerignore": true,
}

var diretivasGo = []string{"//go:", "//line ", "// +build", "//nolint", "//export "}

func ehModelo(nome string) bool {
	return strings.HasSuffix(nome, ".example") || strings.HasSuffix(nome, ".exemplo") || strings.HasSuffix(nome, ".md")
}

func verificar(raiz string) ([]string, error) {
	var achados []string
	err := filepath.WalkDir(raiz, func(caminho string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		nome := d.Name()
		if d.IsDir() {
			if caminho != raiz && diretoriosIgnorados[nome] {
				return filepath.SkipDir
			}
			return nil
		}
		if ehModelo(nome) {
			return nil
		}
		relativo, _ := filepath.Rel(raiz, caminho)
		switch {
		case strings.HasSuffix(nome, ".go"):
			linhas, err := comentariosGo(caminho)
			if err != nil {
				return err
			}
			for _, l := range linhas {
				achados = append(achados, fmt.Sprintf("%s:%d", relativo, l))
			}
		case extensoesDeHash[filepath.Ext(nome)] || nomesDeHash[nome]:
			linhas, err := comentariosDeHash(caminho, filepath.Ext(nome) == ".ps1")
			if err != nil {
				return err
			}
			for _, l := range linhas {
				achados = append(achados, fmt.Sprintf("%s:%d", relativo, l))
			}
		}
		return nil
	})
	return achados, err
}

func comentariosGo(caminho string) ([]int, error) {
	fonte, err := os.ReadFile(caminho)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	arquivo := fset.AddFile(caminho, -1, len(fonte))
	var s scanner.Scanner
	s.Init(arquivo, fonte, nil, scanner.ScanComments)
	var linhas []int
	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		if tok != token.COMMENT || ehDiretivaGo(lit) {
			continue
		}
		linhas = append(linhas, fset.Position(pos).Line)
	}
	return linhas, nil
}

func ehDiretivaGo(comentario string) bool {
	for _, d := range diretivasGo {
		if strings.HasPrefix(comentario, d) {
			return true
		}
	}
	return false
}

func comentariosDeHash(caminho string, powershell bool) ([]int, error) {
	fonte, err := os.ReadFile(caminho)
	if err != nil {
		return nil, err
	}
	var linhas []int
	dentroDeHereString := false
	leitor := bufio.NewScanner(bytes.NewReader(fonte))
	leitor.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for n := 1; leitor.Scan(); n++ {
		bruta := leitor.Text()
		limpa := strings.TrimSpace(bruta)
		if powershell {
			if dentroDeHereString {
				if strings.HasPrefix(bruta, `"@`) || strings.HasPrefix(bruta, `'@`) {
					dentroDeHereString = false
				}
				continue
			}
			if strings.HasSuffix(limpa, `@"`) || strings.HasSuffix(limpa, `@'`) {
				dentroDeHereString = true
				continue
			}
		}
		if !strings.HasPrefix(limpa, "#") {
			continue
		}
		if n == 1 && (strings.HasPrefix(limpa, "#!") || strings.HasPrefix(limpa, "# syntax=")) {
			continue
		}
		linhas = append(linhas, n)
	}
	return linhas, leitor.Err()
}

func raizDoRepositorio(t *testing.T) string {
	t.Helper()
	raiz, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(raiz, "go.mod")); err != nil {
		t.Fatalf("raiz do repositório não encontrada a partir de %s: %v", raiz, err)
	}
	return raiz
}

func TestRepositorioSemComentario(t *testing.T) {
	achados, err := verificar(raizDoRepositorio(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(achados) > 0 {
		t.Errorf("zero comentário em código é regra do projeto; %d encontrados:\n%s",
			len(achados), strings.Join(achados, "\n"))
	}
}

func escrever(t *testing.T, raiz, caminho, conteudo string) {
	t.Helper()
	completo := filepath.Join(raiz, caminho)
	if err := os.MkdirAll(filepath.Dir(completo), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(completo, []byte(conteudo), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestVerificadorAcusaComentario(t *testing.T) {
	raiz := t.TempDir()
	escrever(t, raiz, "a.go", "package a\n\n// explica\nvar x = 1 // ao lado\n\nvar y = `// dentro de string`\n")
	escrever(t, raiz, "b.go", "package a\n\n/* bloco */\nvar z = 2\n")
	escrever(t, raiz, "s.sh", "#!/bin/bash\n# comentario\necho '# texto'\n")
	escrever(t, raiz, "Dockerfile", "# syntax=docker/dockerfile:1\nFROM x\n  # recuado\n")
	escrever(t, raiz, "ci.yml", "a: 1\n# nota\n")
	escrever(t, raiz, "p.ps1", "# topo\n$x = 1\n")

	achados, err := verificar(raiz)
	if err != nil {
		t.Fatal(err)
	}
	esperados := []string{"a.go:3", "a.go:4", "b.go:3", "s.sh:2", "Dockerfile:3", "ci.yml:2", "p.ps1:1"}
	slices.Sort(achados)
	slices.Sort(esperados)
	if !slices.Equal(achados, esperados) {
		t.Errorf("achados = %v, esperados %v", achados, esperados)
	}
}

func TestVerificadorRespeitaExcecoes(t *testing.T) {
	raiz := t.TempDir()
	escrever(t, raiz, "e.go", "package e\n\nimport _ \"embed\"\n\n//go:embed x.txt\nvar x string\n")
	escrever(t, raiz, "run.sh", "#!/usr/bin/env bash\necho ok\n")
	escrever(t, raiz, "Dockerfile", "# syntax=docker/dockerfile:1\nFROM x\n")
	escrever(t, raiz, "i.ps1", "$c = @\"\n# linha do arquivo gerado\nA=1\n\"@\n$d = @'\n# outra\n'@\n")
	escrever(t, raiz, ".env.example", "# documentação do modelo\nA=1\n")
	escrever(t, raiz, "k.exemplo", "# modelo\n")
	escrever(t, raiz, "LEIA.md", "# título\n")
	escrever(t, raiz, "node_modules/x/y.go", "package y // de terceiro\n")

	achados, err := verificar(raiz)
	if err != nil {
		t.Fatal(err)
	}
	if len(achados) != 0 {
		t.Errorf("exceções acusadas como comentário: %v", achados)
	}
}
