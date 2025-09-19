package gen

import (
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

//
// structures for .vcxproj
//

type VSProject struct {
	XMLName              xml.Name                `xml:"Project"`
	DefaultTargets       string                  `xml:"DefaultTargets,attr"`
	ToolsVersion         string                  `xml:"ToolsVersion,attr"`
	XMLNS                string                  `xml:"xmlns,attr"`
	PropertyGroups       []VSPropertyGroup       `xml:"PropertyGroup"`
	ItemGroups           []VSItemGroup           `xml:"ItemGroup"`
	ImportGroups         []VSImportGroup         `xml:"ImportGroup"`
	ItemDefinitionGroups []VSItemDefinitionGroup `xml:"ItemDefinitionGroup"`
	Imports              []VSImport              `xml:"Import"`
}

type VSItemGroup struct {
	Label                 string                   `xml:"Label,attr,omitempty"`
	ProjectConfigurations []VSProjectConfiguration `xml:"ProjectConfiguration,omitempty"`
	ClCompiles            []VSClCompile            `xml:"ClCompile,omitempty"`
	ProjectReferences     []VSProjectReference     `xml:"ProjectReference,omitempty"`
}

type VSProjectConfiguration struct {
	Include       string `xml:"Include,attr"`
	Configuration string `xml:"Configuration"`
	Platform      string `xml:"Platform"`
}

type VSClCompile struct {
	Include string `xml:"Include,attr"`
}

type VSProjectReference struct {
	Include                 string `xml:"Include,attr"`
	Project                 string `xml:"Project"`
	Name                    string `xml:"Name"`
	LinkLibraryDependencies bool   `xml:"LinkLibraryDependencies"`
}

type VSPropertyGroup struct {
	Label                        string `xml:"Label,attr,omitempty"`
	Condition                    string `xml:"Condition,attr,omitempty"`
	PreferredToolArchitecture    string `xml:"PreferredToolArchitecture,omitempty"`
	ProjectGuid                  string `xml:"ProjectGuid,omitempty"`
	Keyword                      string `xml:"Keyword,omitempty"`
	WindowsTargetPlatformVersion string `xml:"WindowsTargetPlatformVersion,omitempty"`
	ProjectName                  string `xml:"ProjectName,omitempty"`
	ConfigurationType            string `xml:"ConfigurationType,omitempty"`
	PlatformToolset              string `xml:"PlatformToolset,omitempty"`
	CharacterSet                 string `xml:"CharacterSet,omitempty"`
	OutDir                       string `xml:"OutDir,omitempty"`
	IntDir                       string `xml:"IntDir,omitempty"`
	TargetName                   string `xml:"TargetName,omitempty"`
	TargetExt                    string `xml:"TargetExt,omitempty"`
	LinkIncremental              *bool  `xml:"LinkIncremental,omitempty"`
	GenerateManifest             bool   `xml:"GenerateManifest,omitempty"`
	UseDebugLibraries            *bool  `xml:"UseDebugLibraries,omitempty"`
	WholeProgramOptimization     *bool  `xml:"WholeProgramOptimization,omitempty"`
}

type VSImportGroup struct {
	Label   string     `xml:"Label,attr,omitempty"`
	Imports []VSImport `xml:"Import"`
}

type VSImport struct {
	Project   string `xml:"Project,attr"`
	Condition string `xml:"Condition,attr,omitempty"`
	Label     string `xml:"Label,attr,omitempty"`
}

type VSItemDefinitionGroup struct {
	Condition string          `xml:"Condition,attr"`
	ClCompile VSCppCompileDef `xml:"ClCompile"`
	Link      VSLinkDef       `xml:"Link"`
}

type VSCppCompileDef struct {
	WarningLevel                 string `xml:"WarningLevel"`
	SDLCheck                     bool   `xml:"SDLCheck"`
	AdditionalIncludeDirectories string `xml:"AdditionalIncludeDirectories"`
	PreprocessorDefinitions      string `xml:"PreprocessorDefinitions"`
	ConformanceMode              bool   `xml:"ConformanceMode"`
	Optimization                 string `xml:"Optimization,omitempty"`
	BasicRuntimeChecks           string `xml:"BasicRuntimeChecks,omitempty"`
	DebugInformationFormat       string `xml:"DebugInformationFormat,omitempty"`
	RuntimeLibrary               string `xml:"RuntimeLibrary,omitempty"`
	FunctionLevelLinking         *bool  `xml:"FunctionLevelLinking,omitempty"`
	IntrinsicFunctions           *bool  `xml:"IntrinsicFunctions,omitempty"`
}

type VSLinkDef struct {
	SubSystem                string `xml:"SubSystem"`
	GenerateDebugInformation *bool  `xml:"GenerateDebugInformation,omitempty"`
	AdditionalDependencies   string `xml:"AdditionalDependencies"`
	ProgramDataBaseFile      string `xml:"ProgramDataBaseFile,omitempty"`
	ImportLibrary            string `xml:"ImportLibrary,omitempty"`
	AdditionalOptions        string `xml:"AdditionalOptions,omitempty"`
	EnableCOMDATFolding      *bool  `xml:"EnableCOMDATFolding,omitempty"`
	OptimizeReferences       *bool  `xml:"OptimizeReferences,omitempty"`
}

type VSFiltersProject struct {
	XMLName      xml.Name             `xml:"Project"`
	ToolsVersion string               `xml:"ToolsVersion,attr"`
	XMLNS        string               `xml:"xmlns,attr"`
	ItemGroups   []VSFiltersItemGroup `xml:"ItemGroup"`
}

type VSFiltersItemGroup struct {
	ClCompiles []VSFiltersClCompile `xml:"ClCompile,omitempty"`
	Filters    []VSFiltersFilter    `xml:"Filter,omitempty"`
}

type VSFiltersClCompile struct {
	Include string `xml:"Include,attr"`
	Filter  string `xml:"Filter"`
}

type VSFiltersFilter struct {
	Include          string `xml:"Include,attr"`
	UniqueIdentifier string `xml:"UniqueIdentifier"`
	Extensions       string `xml:"Extensions"`
}

//
// generator
//

type VS2022Gen struct {
	targets map[string]buildUnit
}

func NewVS2022Gen() *VS2022Gen {
	return &VS2022Gen{
		targets: make(map[string]buildUnit),
	}
}

func (g *VS2022Gen) SetCompiler(cc, cxx string) {}

func (g *VS2022Gen) BuildFile() string {
	var solutionName string
	for name, target := range g.targets {
		if !target.isLib {
			solutionName = name
			break
		}
	}
	if solutionName == "" {
		for name := range g.targets {
			return name + ".sln"
		}
	}
	return solutionName + ".sln"
}

func (g *VS2022Gen) AddTarget(name, basedir string, sources, dependencies []string, isLib bool, cflags, ldflags []string) {
	if g.targets == nil {
		g.targets = make(map[string]buildUnit)
	}
	targetSources := make([]sourceFile, 0, len(sources))
	for _, srcPath := range sources {
		targetSources = append(targetSources, sourceFile{src: srcPath, isCxx: isCxx(srcPath)})
	}
	g.targets[name] = buildUnit{
		name:         name,
		isLib:        isLib,
		sources:      targetSources,
		dependencies: dependencies,
		cflags:       cflags,
		ldflags:      ldflags,
		basedir:      basedir,
	}
}

func (g *VS2022Gen) Generate() string {
	projectGuids := make(map[string]string)
	for name := range g.targets {
		projectGuids[name] = strings.ToUpper(uuid.New().String())
	}

	for name, target := range g.targets {
		buildDir := filepath.Join(target.basedir, "build")
		projectDir := filepath.Join(buildDir, name)
		os.MkdirAll(projectDir, 0755)

		g.generateProjectFile(buildDir, projectDir, name, target, projectGuids)
		g.generateFiltersFile(projectDir, name, target)
	}

	return g.generateSolutionFile(projectGuids)
}

func (g *VS2022Gen) generateSolutionFile(projectGuids map[string]string) string {
	solutionGuid := strings.ToUpper(uuid.New().String())
	var sb strings.Builder

	writeln(&sb, "Microsoft Visual Studio Solution File, Format Version 12.00")
	writeln(&sb, "# Visual Studio Version 17")
	for name, guid := range projectGuids {
		// Windows (Visual C++) https://github.com/VISTALL/visual-studio-project-type-guids
		writeln(&sb,
			`Project("{8BC9CEB8-8B4A-11D0-8D11-00A0C91BC942}") = "`, name, `", "`, name, `\`, name, `.vcxproj", "{`, guid, `}"`,
		)
		writeln(&sb, "EndProject")
	}
	writeln(&sb, "Global")
	writeln(&sb, "\tGlobalSection(SolutionConfigurationPlatforms) = preSolution")
	writeln(&sb, "\t\tDebug|x64 = Debug|x64")
	writeln(&sb, "\t\tRelease|x64 = Release|x64")
	writeln(&sb, "\tEndGlobalSection")
	writeln(&sb, "\tGlobalSection(ProjectConfigurationPlatforms) = postSolution")
	for _, guid := range projectGuids {
		writeln(&sb, "\t\t{", guid, "}.Debug|x64.ActiveCfg = Debug|x64")
		writeln(&sb, "\t\t{", guid, "}.Debug|x64.Build.0 = Debug|x64")
		writeln(&sb, "\t\t{", guid, "}.Release|x64.ActiveCfg = Release|x64")
		writeln(&sb, "\t\t{", guid, "}.Release|x64.Build.0 = Release|x64")
	}
	writeln(&sb, "\tEndGlobalSection")
	writeln(&sb, "\tGlobalSection(SolutionProperties) = preSolution")
	writeln(&sb, "\t\tHideSolutionNode = FALSE")
	writeln(&sb, "\tEndGlobalSection")
	writeln(&sb, "\tGlobalSection(ExtensibilityGlobals) = postSolution")
	writeln(&sb, "\t\tSolutionGuid = {", solutionGuid, "}")
	writeln(&sb, "\tEndGlobalSection")
	writeln(&sb, "EndGlobal")

	return sb.String()
}

func (g *VS2022Gen) generateProjectFile(buildDir, projectDir, name string, target buildUnit, projectGuids map[string]string) error {
	clCompiles := make([]VSClCompile, 0, len(target.sources))
	for _, source := range target.sources {
		relPath, _ := filepath.Rel(projectDir, source.src)
		clCompiles = append(clCompiles, VSClCompile{Include: relPath})
	}

	projectRefs := make([]VSProjectReference, 0, len(target.dependencies))
	for _, depName := range target.dependencies {
		projectRefs = append(projectRefs, VSProjectReference{
			Include:                 fmt.Sprintf(`..\%s\%s.vcxproj`, depName, depName),
			Project:                 "{" + projectGuids[depName] + "}",
			Name:                    depName,
			LinkLibraryDependencies: true,
		})
	}

	allPropertyGroups := []VSPropertyGroup{
		{PreferredToolArchitecture: "x64"},
	}
	allPropertyGroups = append(allPropertyGroups, g.createGlobalPropertyGroups(name, projectGuids[name])...)
	allPropertyGroups = append(allPropertyGroups, g.createConfigurationPropertyGroups(target, buildDir)...)

	allItemGroups := []VSItemGroup{
		{
			Label: "ProjectConfigurations",
			ProjectConfigurations: []VSProjectConfiguration{
				{Include: "Debug|x64", Configuration: "Debug", Platform: "x64"},
				{Include: "Release|x64", Configuration: "Release", Platform: "x64"},
			},
		},
	}
	allItemGroups = append(allItemGroups, g.createSourceItemGroups(clCompiles)...)
	allItemGroups = append(allItemGroups, VSItemGroup{ProjectReferences: projectRefs})

	allImports := []VSImport{
		{Project: `$(VCTargetsPath)\Microsoft.Cpp.Default.props`},
	}
	allImports = append(allImports, g.createStandardImports()...)
	allImports = append(allImports, VSImport{Project: `$(VCTargetsPath)\Microsoft.Cpp.targets`})

	project := VSProject{
		DefaultTargets:       "Build",
		ToolsVersion:         "17.0",
		XMLNS:                "http://schemas.microsoft.com/developer/msbuild/2003",
		PropertyGroups:       allPropertyGroups,
		ItemGroups:           allItemGroups,
		ItemDefinitionGroups: g.createItemDefinitionGroups(target),
		Imports:              allImports,
		ImportGroups:         []VSImportGroup{{Label: "ExtensionTargets"}},
	}

	output, err := xml.MarshalIndent(project, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(projectDir, name+".vcxproj"), []byte(xml.Header+string(output)), 0644)
}

func (g *VS2022Gen) createGlobalPropertyGroups(name, guid string) []VSPropertyGroup {
	return []VSPropertyGroup{
		{
			Label:                        "Globals",
			ProjectGuid:                  "{" + guid + "}",
			Keyword:                      "Win32Proj",
			WindowsTargetPlatformVersion: "10.0",
			ProjectName:                  name,
		},
	}
}

func (g *VS2022Gen) createConfigurationPropertyGroups(target buildUnit, buildDir string) []VSPropertyGroup {
	trueVal, falseVal := true, false
	debugOutDir := filepath.Join(buildDir, "Debug") + `\`
	releaseOutDir := filepath.Join(buildDir, "Release") + `\`
	debugIntDir := filepath.Join(target.basedir, "build", target.name, "int", "Debug") + `\`
	releaseIntDir := filepath.Join(target.basedir, "build", target.name, "int", "Release") + `\`

	return []VSPropertyGroup{
		{
			Condition:         "'$(Configuration)|$(Platform)'=='Debug|x64'",
			Label:             "Configuration",
			ConfigurationType: getConfigurationType(target.isLib),
			PlatformToolset:   "v143",
			CharacterSet:      "Unicode",
			UseDebugLibraries: &trueVal,
		},
		{
			Condition:                "'$(Configuration)|$(Platform)'=='Release|x64'",
			Label:                    "Configuration",
			ConfigurationType:        getConfigurationType(target.isLib),
			PlatformToolset:          "v143",
			CharacterSet:             "Unicode",
			UseDebugLibraries:        &falseVal,
			WholeProgramOptimization: &trueVal,
		},
		{
			Condition:        "'$(Configuration)|$(Platform)'=='Debug|x64'",
			OutDir:           debugOutDir,
			IntDir:           debugIntDir,
			TargetName:       target.name,
			TargetExt:        getTargetExt(target.isLib),
			LinkIncremental:  &trueVal,
			GenerateManifest: true,
		},
		{
			Condition:        "'$(Configuration)|$(Platform)'=='Release|x64'",
			OutDir:           releaseOutDir,
			IntDir:           releaseIntDir,
			TargetName:       target.name,
			TargetExt:        getTargetExt(target.isLib),
			LinkIncremental:  &falseVal,
			GenerateManifest: true,
		},
	}
}

func (g *VS2022Gen) createItemDefinitionGroups(target buildUnit) []VSItemDefinitionGroup {
	trueVal, falseVal := true, false
	return []VSItemDefinitionGroup{
		{
			Condition: "'$(Configuration)|$(Platform)'=='Debug|x64'",
			ClCompile: VSCppCompileDef{
				WarningLevel:                 "Level3",
				SDLCheck:                     true,
				AdditionalIncludeDirectories: parseIncludes(target.cflags),
				PreprocessorDefinitions:      parseDefines(target.cflags, true),
				ConformanceMode:              true,
				Optimization:                 "Disabled",
				BasicRuntimeChecks:           "EnableFastChecks",
				DebugInformationFormat:       "ProgramDatabase",
				RuntimeLibrary:               "MultiThreadedDebugDLL",
			},
			Link: VSLinkDef{
				SubSystem:                "Windows",
				GenerateDebugInformation: &trueVal,
				AdditionalDependencies:   parseLibraries(target.ldflags, !target.isLib),
				ProgramDataBaseFile:      `$(OutDir)$(TargetName).pdb`,
				AdditionalOptions:        "%(AdditionalOptions) /machine:x64",
			},
		},
		{
			Condition: "'$(Configuration)|$(Platform)'=='Release|x64'",
			ClCompile: VSCppCompileDef{
				WarningLevel:                 "Level3",
				SDLCheck:                     true,
				AdditionalIncludeDirectories: parseIncludes(target.cflags),
				PreprocessorDefinitions:      parseDefines(target.cflags, false),
				ConformanceMode:              true,
				Optimization:                 "MaxSpeed",
				RuntimeLibrary:               "MultiThreadedDLL",
				FunctionLevelLinking:         &trueVal,
				IntrinsicFunctions:           &trueVal,
			},
			Link: VSLinkDef{
				SubSystem:                "Windows",
				GenerateDebugInformation: &falseVal,
				AdditionalDependencies:   parseLibraries(target.ldflags, !target.isLib),
				EnableCOMDATFolding:      &trueVal,
				OptimizeReferences:       &trueVal,
				ProgramDataBaseFile:      `$(OutDir)$(TargetName).pdb`,
				AdditionalOptions:        "%(AdditionalOptions) /machine:x64",
			},
		},
	}
}

func (g *VS2022Gen) createSourceItemGroups(clCompiles []VSClCompile) []VSItemGroup {
	return []VSItemGroup{{ClCompiles: clCompiles}}
}

func (g *VS2022Gen) createStandardImports() []VSImport {
	return []VSImport{
		{Project: `$(VCTargetsPath)\Microsoft.Cpp.props`},
		{Project: `$(UserRootDir)\Microsoft.Cpp.$(Platform).user.props`, Condition: `exists('$(UserRootDir)\Microsoft.Cpp.$(Platform).user.props')`, Label: "LocalAppDataPlatform"},
	}
}

func (g *VS2022Gen) generateFiltersFile(projectDir, name string, target buildUnit) error {
	clCompiles := make([]VSFiltersClCompile, 0, len(target.sources))
	for _, source := range target.sources {
		relPath, _ := filepath.Rel(projectDir, source.src)
		clCompiles = append(clCompiles, VSFiltersClCompile{Include: relPath, Filter: "Source Files"})
	}
	filters := VSFiltersProject{
		ToolsVersion: "17.0",
		XMLNS:        "http://schemas.microsoft.com/developer/msbuild/2003",
		ItemGroups: []VSFiltersItemGroup{
			{ClCompiles: clCompiles},
			{Filters: []VSFiltersFilter{{Include: "Source Files", UniqueIdentifier: "{" + strings.ToUpper(uuid.New().String()) + "}", Extensions: "cpp;c;cc;cxx;c++;cppm;ixx;def;odl;idl;hpj;bat;asm;asmx"}}},
		},
	}
	output, err := xml.MarshalIndent(filters, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(projectDir, name+".vcxproj.filters"), []byte(xml.Header+string(output)), 0644)
}

func (g *VS2022Gen) Invoke(buildDir string) error {
	msbuild, err := FindMsbuild()
	if err != nil {
		return err
	}

	cmd := exec.Command(msbuild, g.BuildFile())
	cmd.Dir = buildDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func getConfigurationType(isLib bool) string {
	if isLib {
		return "StaticLibrary"
	}
	return "Application"
}

func getTargetExt(isLib bool) string {
	if isLib {
		return ".lib"
	}
	return ".exe"
}

func parseIncludes(cflags []string) string {
	var includes []string
	for _, flag := range cflags {
		if after, ok := strings.CutPrefix(flag, "-I"); ok {
			includes = append(includes, after)
		}
	}
	return strings.Join(includes, ";") + ";%(AdditionalIncludeDirectories)"
}

func parseDefines(cflags []string, isDebug bool) string {
	defines := []string{"WIN32", "_WINDOWS"}
	if isDebug {
		defines = append(defines, "_DEBUG")
	} else {
		defines = append(defines, "NDEBUG")
	}
	for _, flag := range cflags {
		if after, ok := strings.CutPrefix(flag, "-D"); ok {
			defines = append(defines, after)
		}
	}
	return strings.Join(defines, ";") + ";%(PreprocessorDefinitions)"
}

func parseLibraries(ldflags []string, isExe bool) string {
	var libs []string
	if isExe {
		libs = append(libs, "kernel32.lib", "user32.lib", "gdi32.lib", "winspool.lib", "comdlg32.lib", "advapi32.lib", "shell32.lib", "ole32.lib", "oleaut32.lib", "uuid.lib")
	}
	for _, flag := range ldflags {
		if after, ok := strings.CutPrefix(flag, "-l"); ok {
			if strings.HasSuffix(after, ".lib") {
				libs = append(libs, after)
			} else {
				libs = append(libs, after+".lib")
			}
		}
	}
	return strings.Join(libs, ";") + ";%(AdditionalDependencies)"
}
