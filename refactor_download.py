import sys

with open('webui/templates/public/download.html', 'r') as f:
    lines = f.readlines()

# find index of /* Content Area */
start_css_idx = 0
for i, line in enumerate(lines):
    if "/* Content Area */" in line:
        start_css_idx = i
        break

# find index of /* Footer */
end_css_idx = 0
for i, line in enumerate(lines):
    if "/* Footer */" in line:
        end_css_idx = i
        break

# find index of <main id="main-content">
main_start_idx = 0
for i, line in enumerate(lines):
    if '<main id="main-content">' in line:
        main_start_idx = i
        break

# find index of <footer>
footer_start_idx = 0
for i, line in enumerate(lines):
    if '<footer' in line:
        footer_start_idx = i
        break

new_html = []
new_html.append('{{ define "extra_head" }}\n')
new_html.append('<style>\n')
new_html.extend(lines[start_css_idx:end_css_idx])
new_html.append('</style>\n')
new_html.append('{{ end }}\n')
new_html.append('{{ template "public_head" . }}\n')
new_html.extend(lines[main_start_idx:footer_start_idx])

# Now the scripts
script_start_idx = 0
for i, line in enumerate(lines):
    if '<script>' in line and i > footer_start_idx:
        script_start_idx = i
        break

new_html.append('{{ define "extra_scripts" }}\n')
new_html.extend(lines[script_start_idx:])
# find </body> and remove it and </html>
for i in range(len(new_html)-1, -1, -1):
    if '</body>' in new_html[i] or '</html>' in new_html[i]:
        new_html.pop(i)

new_html.append('{{ end }}\n')
new_html.append('{{ template "public_foot" . }}\n')

with open('webui/templates/public/download.html', 'w') as f:
    f.writelines(new_html)
