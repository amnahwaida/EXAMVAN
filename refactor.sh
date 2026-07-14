#!/bin/bash

# Create shared.html
echo '{{ define "public_head" }}' > webui/templates/public/shared.html
head -n 837 webui/templates/public/index.html >> webui/templates/public/shared.html
echo '{{ end }}' >> webui/templates/public/shared.html

echo '{{ define "public_foot" }}' >> webui/templates/public/shared.html
tail -n 25 webui/templates/public/index.html >> webui/templates/public/shared.html
echo '<script>' >> webui/templates/public/shared.html
echo 'document.addEventListener("DOMContentLoaded", function() {' >> webui/templates/public/shared.html
echo '  var path = window.location.pathname;' >> webui/templates/public/shared.html
echo '  document.querySelectorAll(".nav-link").forEach(function(link) {' >> webui/templates/public/shared.html
echo '    if (link.getAttribute("href") === path) link.classList.add("active");' >> webui/templates/public/shared.html
echo '  });' >> webui/templates/public/shared.html
echo '});' >> webui/templates/public/shared.html
echo '</script>' >> webui/templates/public/shared.html
echo '{{ end }}' >> webui/templates/public/shared.html

# Prepare index.html
echo '{{ template "public_head" . }}' > webui/templates/public/index.html.new
tail -n +838 webui/templates/public/index.html | head -n -25 >> webui/templates/public/index.html.new
echo '{{ template "public_foot" . }}' >> webui/templates/public/index.html.new
mv webui/templates/public/index.html.new webui/templates/public/index.html
