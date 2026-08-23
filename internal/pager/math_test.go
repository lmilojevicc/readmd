package pager

import "testing"

func TestTexSubTable(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`\alpha+\beta`, "α+β"},
		{`\gamma\delta\epsilon\zeta`, "γδεζ"},
		{`\eta\theta\iota\kappa`, "ηθικ"},
		{`\lambda\mu\nu\xi`, "λμνξ"},
		{`\omicron\pi\rho\sigma\tau`, "οπρστ"},
		{`\upsilon\phi\chi\psi\omega`, "υφχψω"},
		{`\Gamma\Delta\Theta\Lambda\Xi`, "ΓΔΘΛΞ"},
		{`\Pi\Sigma\Upsilon\Phi\Psi\Omega`, "ΠΣΥΦΨΩ"},
		{`\sum_{i=1}^{n}`, "∑ᵢ₌₁ⁿ"},
		{`\prod \int \infty`, "∏ ∫ ∞"},
		{`a \pm b`, "a ± b"},
		{`2 \times 3`, "2 × 3"},
		{`x \cdot y`, "x · y"},
		{`6 \div 2`, "6 ÷ 2"},
		{`1 \leq 2 \geq 0`, "1 ≤ 2 ≥ 0"},
		{`a \neq b`, "a ≠ b"},
		{`x \approx y`, "x ≈ y"},
		{`a \equiv b`, "a ≡ b"},
		{`x \rightarrow y`, "x → y"},
		{`x \leftarrow y`, "x ← y"},
		{`A \Rightarrow B`, "A ⇒ B"},
		{`A \Leftrightarrow B`, "A ⇔ B"},
		{`x \mapsto f(x)`, "x ↦ f(x)"},
		{`x \in S`, "x ∈ S"},
		{`x \notin S`, "x ∉ S"},
		{`A \subset B`, "A ⊂ B"},
		{`A \subseteq B`, "A ⊆ B"},
		{`A \cup B`, "A ∪ B"},
		{`A \cap B`, "A ∩ B"},
		{`\forall x \exists y`, "∀ x ∃ y"},
		{`\partial f`, "∂ f"},
		{`\nabla v`, "∇ v"},
		{`a\;b`, "a b"},
		{`a\quad b`, "a  b"},
		{`a\,b`, "a b"},
		{`\alpha\,\beta`, "α β"},
		{`\left( x \right)`, "( x )"},
		{`x\!y`, "xy"},
		{`\sqrt{x}`, "√(x)"},
		{`\sqrt{x+1}`, "√(x+1)"},
		{`\frac{a}{b}`, "(a/b)"},
		{`\frac{dy}{dx}`, "(dy/dx)"},
		{`x^{2}`, "x²"},
		{`x^2`, "x²"},
		{`v^{n+1}`, "vⁿ⁺¹"},
		{`T^{-1}`, "T⁻¹"},
		{`x_i`, "xᵢ"},
		{`a_{ij}`, "aᵢⱼ"},
		{`x_{10}`, "x₁₀"},
		{`E=mc^2`, "E=mc²"},
		{`x^{a+b}`, "xᵃ⁺ᵇ"},
		{`x_{zz}`, "x_(zz)"},
		{`{x+y}`, "x+y"},
		{`2^{2^3}`, "2^(2³)"},
		{`\unknown`, `\unknown`},
		{`\foo bar`, `\foo bar`},
		{`\sum\unknown\prod`, "∑\\unknown∏"},
		{`a \to b`, `a \to b`},
	} {
		if got := texSub(tc.in); got != tc.want {
			t.Errorf("texSub(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSubstituteMathScanRules(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"inline math", "value $x^2$ end", "value x² end"},
		{"block math", "$$\\alpha$$ tail", "α tail"},
		{"unclosed dollar untouched", "cost $5 and", "cost $5 and"},
		{"currency pair untouched", "$5 and$10 more", "$5 and$10 more"},
		{"spaced closer untouched", "between $5 and $6 ok", "between $5 and $6 ok"},
		{"empty math untouched", "a $$ b", "a $$ b"},
		{"escaped dollars untouched", `\$5 and \$6 here`, `\$5 and \$6 here`},
		{"no-op substitution skipped", "lit $5$ lit", "lit $5$ lit"},
		{"multiline block", "$$\n\\alpha\n\\beta\n$$\nx", "α\nβ\nx"},
		{"no-op inline skipped", "f $a + b$ g", "f $a + b$ g"},
		{"two inline maths", "$a_1$ then $b^2$", "a₁ then b²"},
		{"dollar digit after closer rejected", "$a$5 rest", "$a$5 rest"},
		{"tight list item", "- item $x^2$", "- item x²"},
		{"protected span inside inline rejected", "$\\alpha `mid` \\beta$ tail", "$\\alpha `mid` \\beta$ tail"},
		{"softbreak pair rejected", "a $x^2\ny$ b", "a $x^2\ny$ b"},
		{"link destination untouched", "see [docs](http://e.com/$x^2$.png) and $a^2$ ok",
			"see [docs](http://e.com/$x^2$.png) and a² ok"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := substituteMath(tc.in + "\n"); got != tc.want+"\n" {
				t.Errorf("got %q, want %q", got, tc.want+"\n")
			}
		})
	}
}

func TestMathCodeImmunity(t *testing.T) {
	for _, tc := range []struct{ name, in string }{
		{"code span", "keep `$x^2$` literal\n"},
		{"fenced block", "```\n$x^2$\n```\n"},
		{"language fence", "```go\nx := $a^2$\n```\n"},
		{"indented code", "text:\n\n    $x^2$\n"},
		{"autolink url", "<https://example.com/$a$b>\n"},
		{"html block", "<div>$x^2$</div>\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := substituteMath(tc.in); got != tc.in {
				t.Errorf("protected region mutated:\ngot  %q\nwant %q", got, tc.in)
			}
		})
	}
}

func TestMathStillSubstitutesOutsideProtected(t *testing.T) {
	in := "`$a^2$` then $b^2$ end\n"
	got := substituteMath(in)
	want := "`$a^2$` then b² end\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
