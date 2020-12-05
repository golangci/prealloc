package prealloc

import (
	"flag"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// Support: (in order of priority)
//  * Full make suggestion with type?
//	* Test flag
//  * Embedded ifs?
//  * Use an import rather than the duplcated import.go

const (
	pwd = "./"
)

func usage() {
	log.Printf("Usage of %s:\n", os.Args[0])
	log.Printf("\nprealloc [flags] # runs on package in current directory\n")
	log.Printf("\nprealloc [flags] [packages]\n")
	log.Printf("Flags:\n")
	flag.PrintDefaults()
}

type sliceDeclaration struct {
	name string
	// sType string
	genD *ast.GenDecl
}

type returnsVisitor struct {
	// flags
	simple            bool
	includeRangeLoops bool
	includeForLoops   bool
	// visitor fields
	sliceDeclarations   []*sliceDeclaration
	preallocHints       []Hint
	returnsInsideOfLoop bool
	arrayTypes          []string
}

func NoMain() {

	// Remove log timestamp
	log.SetFlags(0)

	simple := flag.Bool("simple", true, "Report preallocation suggestions only on simple loops that have no returns/breaks/continues/gotos in them")
	includeRangeLoops := flag.Bool("rangeloops", true, "Report preallocation suggestions on range loops")
	includeForLoops := flag.Bool("forloops", false, "Report preallocation suggestions on for loops")
	setExitStatus := flag.Bool("set_exit_status", false, "Set exit status to 1 if any issues are found")
	flag.Usage = usage
	flag.Parse()

	hints, err := checkForPreallocations(flag.Args(), simple, includeRangeLoops, includeForLoops)
	if err != nil {
		log.Println(err)
	}

	for _, hint := range hints {
		log.Println(hint)
	}
	if *setExitStatus && len(hints) > 0 {
		os.Exit(1)
	}
}

func checkForPreallocations(args []string, simple, includeRangeLoops *bool, includeForLoops *bool) ([]Hint, error) {

	fset := token.NewFileSet()

	files, err := parseInput(args, fset)
	if err != nil {
		return nil, fmt.Errorf("could not parse input %v", err)
	}

	hints := []Hint{}
	for _, f := range files {
		retVis := &returnsVisitor{
			simple:            *simple,
			includeRangeLoops: *includeRangeLoops,
			includeForLoops:   *includeForLoops,
		}
		ast.Walk(retVis, f)
		// if simple is true, then we actually have to check if we had returns
		// inside of our loop. Otherwise, we can just report all messages.
		if !retVis.simple || !retVis.returnsInsideOfLoop {
			hints = append(hints, retVis.preallocHints...)
		}
	}

	return hints, nil
}

func Check(files []*ast.File, simple, includeRangeLoops, includeForLoops bool) []Hint {
	hints := []Hint{}
	for _, f := range files {
		retVis := &returnsVisitor{
			simple:            simple,
			includeRangeLoops: includeRangeLoops,
			includeForLoops:   includeForLoops,
		}
		ast.Walk(retVis, f)

		// if simple is true, then we actually have to check if we had returns
		// inside of our loop. Otherwise, we can just report all messages.
		if !retVis.simple || !retVis.returnsInsideOfLoop {
			hints = append(hints, retVis.preallocHints...)
		}
	}

	return hints
}

func parseInput(args []string, fset *token.FileSet) ([]*ast.File, error) {
	var directoryList []string
	var fileMode bool
	files := make([]*ast.File, 0)

	if len(args) == 0 {
		directoryList = append(directoryList, pwd)
	} else {
		for _, arg := range args {
			if strings.HasSuffix(arg, "/...") && isDir(arg[:len(arg)-len("/...")]) {

				for _, dirname := range allPackagesInFS(arg) {
					directoryList = append(directoryList, dirname)
				}

			} else if isDir(arg) {
				directoryList = append(directoryList, arg)

			} else if exists(arg) {
				if strings.HasSuffix(arg, ".go") {
					fileMode = true
					f, err := parser.ParseFile(fset, arg, nil, 0)
					if err != nil {
						return nil, err
					}
					files = append(files, f)
				} else {
					return nil, fmt.Errorf("invalid file %v specified", arg)
				}
			} else {

				//TODO clean this up a bit
				imPaths := importPaths([]string{arg})
				for _, importPath := range imPaths {
					pkg, err := build.Import(importPath, ".", 0)
					if err != nil {
						return nil, err
					}
					var stringFiles []string
					stringFiles = append(stringFiles, pkg.GoFiles...)
					// files = append(files, pkg.CgoFiles...)
					stringFiles = append(stringFiles, pkg.TestGoFiles...)
					if pkg.Dir != "." {
						for i, f := range stringFiles {
							stringFiles[i] = filepath.Join(pkg.Dir, f)
						}
					}

					fileMode = true
					for _, stringFile := range stringFiles {
						f, err := parser.ParseFile(fset, stringFile, nil, 0)
						if err != nil {
							return nil, err
						}
						files = append(files, f)
					}

				}
			}
		}
	}

	// if we're not in file mode, then we need to grab each and every package in each directory
	// we can to grab all the files
	if !fileMode {
		for _, fpath := range directoryList {
			pkgs, err := parser.ParseDir(fset, fpath, nil, 0)
			if err != nil {
				return nil, err
			}

			for _, pkg := range pkgs {
				for _, f := range pkg.Files {
					files = append(files, f)
				}
			}
		}
	}

	return files, nil
}

func isDir(filename string) bool {
	fi, err := os.Stat(filename)
	return err == nil && fi.IsDir()
}

func exists(filename string) bool {
	_, err := os.Stat(filename)
	return err == nil
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}

	return false
}

func (v *returnsVisitor) Visit(node ast.Node) ast.Visitor {

	v.sliceDeclarations = nil
	v.returnsInsideOfLoop = false

	switch n := node.(type) {
	case *ast.TypeSpec:
		if _, ok := n.Type.(*ast.ArrayType); ok {
			if n.Name != nil {
				v.arrayTypes = append(v.arrayTypes, n.Name.Name)
			}
		}
	case *ast.FuncDecl:
		if n.Body != nil {
			for _, stmt := range n.Body.List {
				switch s := stmt.(type) {
				// Find non pre-allocated slices
				case *ast.DeclStmt:
					genD, ok := s.Decl.(*ast.GenDecl)
					if !ok {
						continue
					}
					if genD.Tok == token.TYPE {
						for _, spec := range genD.Specs {
							tSpec, ok := spec.(*ast.TypeSpec)
							if !ok {
								continue
							}

							if _, ok := tSpec.Type.(*ast.ArrayType); ok {
								if tSpec.Name != nil {
									v.arrayTypes = append(v.arrayTypes, tSpec.Name.Name)
								}
							}
						}
					} else if genD.Tok == token.VAR {
						for _, spec := range genD.Specs {
							vSpec, ok := spec.(*ast.ValueSpec)
							if !ok {
								continue
							}
							var isArrType bool
							switch val := vSpec.Type.(type) {
							case *ast.ArrayType:
								isArrType = true
							case *ast.Ident:
								isArrType = contains(v.arrayTypes, val.Name)
							}
							if isArrType {
								if vSpec.Names != nil {
									/*atID, ok := arrayType.Elt.(*ast.Ident)
									if !ok {
										continue
									}*/

									// We should handle multiple slices declared on same line e.g. var mySlice1, mySlice2 []uint32
									for _, vName := range vSpec.Names {
										v.sliceDeclarations = append(v.sliceDeclarations, &sliceDeclaration{name: vName.Name /*sType: atID.Name,*/, genD: genD})
									}
								}
							}
						}
					}

				case *ast.RangeStmt:
					if v.includeRangeLoops {
						if len(v.sliceDeclarations) == 0 {
							continue
						}
						// Check the value being ranged over and ensure it's not a channel (we cannot offer any recommendations on channel ranges).
						rangeIdent, ok := s.X.(*ast.Ident)
						if ok && rangeIdent.Obj != nil {
							valueSpec, ok := rangeIdent.Obj.Decl.(*ast.ValueSpec)
							if ok {
								if _, rangeTargetIsChannel := valueSpec.Type.(*ast.ChanType); rangeTargetIsChannel {
									continue
								}
							}
						}
						if s.Body != nil {
							v.handleLoops(s.Body)
						}
					}

				case *ast.ForStmt:
					if v.includeForLoops {
						if len(v.sliceDeclarations) == 0 {
							continue
						}
						if s.Body != nil {
							v.handleLoops(s.Body)
						}
					}

				default:
				}
			}
		}
	}
	return v
}

// handleLoops is a helper function to share the logic required for both *ast.RangeLoops and *ast.ForLoops
func (v *returnsVisitor) handleLoops(blockStmt *ast.BlockStmt) {

	for _, stmt := range blockStmt.List {
		switch bodyStmt := stmt.(type) {
		case *ast.AssignStmt:
			asgnStmt := bodyStmt
			for index, expr := range asgnStmt.Rhs {
				if index >= len(asgnStmt.Lhs) {
					continue
				}

				lhsIdent, ok := asgnStmt.Lhs[index].(*ast.Ident)
				if !ok {
					continue
				}

				callExpr, ok := expr.(*ast.CallExpr)
				if !ok {
					continue
				}

				rhsFuncIdent, ok := callExpr.Fun.(*ast.Ident)
				if !ok {
					continue
				}

				if rhsFuncIdent.Name != "append" {
					continue
				}

				// e.g., `x = append(x)`
				// Pointless, but pre-allocation will not help.
				if len(callExpr.Args) < 2 {
					continue
				}

				rhsIdent, ok := callExpr.Args[0].(*ast.Ident)
				if !ok {
					continue
				}

				// e.g., `x = append(y, a)`
				// This is weird (and maybe a logic error),
				// but we cannot recommend pre-allocation.
				if lhsIdent.Name != rhsIdent.Name {
					continue
				}

				// e.g., `x = append(x, y...)`
				// we should ignore this. Pre-allocating in this case
				// is confusing, and is not possible in general.
				if callExpr.Ellipsis.IsValid() {
					continue
				}

				for _, sliceDecl := range v.sliceDeclarations {
					if sliceDecl.name == lhsIdent.Name {
						// This is a potential mark, we just need to make sure there are no returns/continues in the
						// range loop.
						// now we just need to grab whatever we're ranging over
						/*sxIdent, ok := s.X.(*ast.Ident)
						if !ok {
							continue
						 */

						v.preallocHints = append(v.preallocHints, Hint{
							Pos:               sliceDecl.genD.Pos(),
							DeclaredSliceName: sliceDecl.name,
						})
					}
				}
			}
		case *ast.IfStmt:
			ifStmt := bodyStmt
			if ifStmt.Body != nil {
				for _, ifBodyStmt := range ifStmt.Body.List {
					// TODO should probably handle embedded ifs here
					switch /*ift :=*/ ifBodyStmt.(type) {
					case *ast.BranchStmt, *ast.ReturnStmt:
						v.returnsInsideOfLoop = true
					default:
					}
				}
			}

		default:

		}
	}

}

// Hint stores the information about an occurrence of a slice that could be
// preallocated.
type Hint struct {
	Pos               token.Pos
	DeclaredSliceName string
}

func (h Hint) String() string {
	return fmt.Sprintf("%v: Consider preallocating %v", h.Pos, h.DeclaredSliceName)
}
