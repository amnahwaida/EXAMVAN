import sys

# Read lines from old_pricing
with open('/tmp/old_pricing.html', 'r') as f:
    old_lines = f.readlines()

# Extract lines 26 to 29 (index 25 to 28)
css_lines = old_lines[25:29]

# Read current pricing.html
with open('webui/templates/public/pricing.html', 'r') as f:
    pricing_lines = f.readlines()

# Create extra_head block
extra_head = ['{{ define "extra_head" }}\n', '<style>\n']
extra_head.extend(css_lines)
extra_head.append('</style>\n')
extra_head.append('{{ end }}\n')

# Insert at the beginning of pricing.html
pricing_lines = extra_head + pricing_lines

with open('webui/templates/public/pricing.html', 'w') as f:
    f.writelines(pricing_lines)

print("Pricing CSS restored.")
