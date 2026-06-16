package commandrisk

import (
	"regexp"
	"sort"
	"strings"

	"github.com/ametel01/agentreceipt/internal/model"
)

type Classification struct {
	Level    model.RiskLevel `json:"level"`
	Signal   string          `json:"signal"`
	Category string          `json:"category"`
	Reason   string          `json:"reason"`
}

type Rule struct {
	Level    model.RiskLevel
	Signal   string
	Category string
	Reason   string
	Pattern  *regexp.Regexp
}

var defaultRules = []Rule{
	{
		Level:    model.RiskHigh,
		Signal:   "privilege_escalation",
		Category: "system",
		Reason:   "command can run with elevated privileges",
		Pattern:  regexp.MustCompile(`(?i)(^|[;&|]\s*)(sudo|su|doas|pkexec)\b`),
	},
	{
		Level:    model.RiskHigh,
		Signal:   "destructive_filesystem",
		Category: "filesystem",
		Reason:   "command can recursively or forcibly delete local files",
		Pattern:  regexp.MustCompile(`(?i)(^|[;&|]\s*)(rm\s+(-[^\s]*[rf][^\s]*|-[^\s]*[f][^\s]*[r])|shred\b|truncate\b|dd\s+)`),
	},
	{
		Level:    model.RiskHigh,
		Signal:   "find_delete",
		Category: "filesystem",
		Reason:   "find -delete can remove many files from a matched tree",
		Pattern:  regexp.MustCompile(`(?i)\bfind\b.*\s-delete\b`),
	},
	{
		Level:    model.RiskHigh,
		Signal:   "secret_access",
		Category: "credentials",
		Reason:   "command appears to read or expose credential material",
		Pattern:  regexp.MustCompile(`(?i)\b(cat|less|more|tail|head|grep|rg)\b.*(\.env\b|\.ssh/|\.aws/credentials|id_rsa|id_ed25519|\.npmrc|\.pypirc|\.netrc)|\b(printenv|env)\b|\b(token|api_key|secret|private_key|password)\b`),
	},
	{
		Level:    model.RiskHigh,
		Signal:   "network_egress",
		Category: "network",
		Reason:   "command can send data to an external host",
		Pattern:  regexp.MustCompile(`(?i)(^|[;&|]\s*)(curl|wget|nc|netcat|scp|sftp|ftp|rsync|ssh|mosh|telnet)\b`),
	},
	{
		Level:    model.RiskHigh,
		Signal:   "cloud_or_deploy_mutation",
		Category: "deployment",
		Reason:   "command can mutate external infrastructure or deployments",
		Pattern:  regexp.MustCompile(`(?i)(^|[;&|]\s*)(aws|gcloud|az|doctl|flyctl|vercel|wrangler|railway|kubectl|helm)\b`),
	},
	{
		Level:    model.RiskHigh,
		Signal:   "package_publish",
		Category: "release",
		Reason:   "command can publish package or release artifacts",
		Pattern:  regexp.MustCompile(`(?i)\b(npm|pnpm|yarn)\s+(publish|npm\s+publish)\b|\bcargo\s+publish\b|\btwine\s+upload\b|\bgoreleaser\s+release\b`),
	},
	{
		Level:    model.RiskHigh,
		Signal:   "destructive_git",
		Category: "git",
		Reason:   "command can discard work or rewrite shared git history",
		Pattern:  regexp.MustCompile(`(?i)\bgit\s+(reset\s+--hard|clean\b|checkout\s+--|restore\b|push\b.*--force|filter-repo\b|filter-branch\b)`),
	},
	{
		Level:    model.RiskHigh,
		Signal:   "container_destructive",
		Category: "container",
		Reason:   "command can delete containers, images, volumes, or cluster resources",
		Pattern:  regexp.MustCompile(`(?i)\bdocker\s+(rm|rmi|system\s+prune|volume\s+rm)\b|\bdocker\s+compose\s+down\b.*\s-v\b|\bkubectl\s+delete\b|\bhelm\s+uninstall\b`),
	},
	{
		Level:    model.RiskMedium,
		Signal:   "dependency_install",
		Category: "dependencies",
		Reason:   "command can change dependencies or execute package lifecycle scripts",
		Pattern:  regexp.MustCompile(`(?i)\b(npm|pnpm|yarn|bun)\s+(install|add)\b|\b(pip|pip3)\s+install\b|\buv\s+add\b|\bpoetry\s+add\b|\bcargo\s+add\b|\bgo\s+get\b`),
	},
	{
		Level:    model.RiskMedium,
		Signal:   "remote_code_execution",
		Category: "network",
		Reason:   "command can fetch and execute code in one step",
		Pattern:  regexp.MustCompile(`(?i)(curl|wget).*(\|\s*(sh|bash|zsh))|\b(npx|pnpm\s+dlx|bunx|pipx\s+run)\b`),
	},
	{
		Level:    model.RiskMedium,
		Signal:   "database_mutation",
		Category: "database",
		Reason:   "command can mutate a database or schema",
		Pattern:  regexp.MustCompile(`(?i)\b(prisma\s+migrate|rails\s+db:migrate|alembic\s+upgrade|psql|mysql|sqlite3)\b`),
	},
	{
		Level:    model.RiskMedium,
		Signal:   "git_mutation",
		Category: "git",
		Reason:   "command mutates local or remote git state",
		Pattern:  regexp.MustCompile(`(?i)\bgit\s+(add|commit|merge|cherry-pick|stash|tag|push|rebase)\b`),
	},
	{
		Level:    model.RiskMedium,
		Signal:   "mass_edit_or_overwrite",
		Category: "filesystem",
		Reason:   "command can rewrite files in place or overwrite paths",
		Pattern:  regexp.MustCompile(`(?i)\b(sed\s+-i|perl\s+-pi|xargs\s+rm)\b|(^|[^>])>\s*(\.env|~|/etc/|/usr/|/var/)`),
	},
	{
		Level:    model.RiskLow,
		Signal:   "script_runner",
		Category: "execution",
		Reason:   "command runs project-defined or interpreter code",
		Pattern:  regexp.MustCompile(`(?i)(^|[;&|]\s*)(make|npm\s+run|pnpm\s+run|yarn\s+run|bun\s+run|python|python3|node|ruby|perl|bash|sh|zsh)\b`),
	},
	{
		Level:    model.RiskLow,
		Signal:   "code_generation",
		Category: "generated_code",
		Reason:   "command can rewrite generated files",
		Pattern:  regexp.MustCompile(`(?i)\b(go\s+generate|buf\s+generate|prisma\s+generate|openapi-generator)\b`),
	},
	{
		Level:    model.RiskLow,
		Signal:   "broad_filesystem_read",
		Category: "filesystem",
		Reason:   "command can reveal broad local file structure or contents",
		Pattern:  regexp.MustCompile(`(?i)(^|[;&|]\s*)(find|tree|ls\s+-la|du\s+-sh)\b`),
	},
}

func Classify(command string) []Classification {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil
	}
	classifications := make([]Classification, 0)
	seen := map[string]bool{}
	for _, rule := range defaultRules {
		if !rule.Pattern.MatchString(command) {
			continue
		}
		key := string(rule.Level) + ":" + rule.Signal
		if seen[key] {
			continue
		}
		seen[key] = true
		classifications = append(classifications, Classification{
			Level:    rule.Level,
			Signal:   rule.Signal,
			Category: rule.Category,
			Reason:   rule.Reason,
		})
	}
	sort.SliceStable(classifications, func(i, j int) bool {
		return riskRank(classifications[i].Level) > riskRank(classifications[j].Level)
	})

	return classifications
}

func riskRank(level model.RiskLevel) int {
	switch level {
	case model.RiskHigh:
		return 3
	case model.RiskMedium:
		return 2
	case model.RiskLow:
		return 1
	default:
		return 0
	}
}
