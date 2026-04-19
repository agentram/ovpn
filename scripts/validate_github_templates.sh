#!/usr/bin/env bash
set -euo pipefail

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
cd "$repo_root"

ruby <<'RUBY'
require 'yaml'

errors = []

parse = lambda do |path|
  begin
    YAML.load_file(path)
  rescue StandardError => e
    errors << "#{path}: YAML parse error: #{e.message}"
    nil
  end
end

validate_form_body = lambda do |path, body, require_non_markdown:|
  unless body.is_a?(Array) && !body.empty?
    errors << "#{path}: body must be a non-empty array"
    return
  end

  labels = {}
  non_markdown = 0
  body.each_with_index do |item, idx|
    unless item.is_a?(Hash)
      errors << "#{path}: body[#{idx}] must be a mapping"
      next
    end
    type = item['type']
    if type.nil? || type.to_s.strip.empty?
      errors << "#{path}: body[#{idx}] missing type"
      next
    end
    if type != 'markdown'
      non_markdown += 1
    end
    attrs = item['attributes']
    if type != 'markdown'
      unless attrs.is_a?(Hash)
        errors << "#{path}: body[#{idx}] attributes must be a mapping for #{type}"
        next
      end
      label = attrs['label']
      if label.to_s.strip.empty?
        errors << "#{path}: body[#{idx}] label is required for #{type}"
      elsif labels.key?(label)
        errors << "#{path}: duplicate body label #{label.inspect}"
      else
        labels[label] = true
      end
    end
  end

  if require_non_markdown && non_markdown.zero?
    errors << "#{path}: body must contain at least one non-markdown field"
  end
end

Dir['.github/ISSUE_TEMPLATE/*.{yml,yaml}'].sort.each do |path|
  doc = parse.call(path)
  next if doc.nil?
  unless doc.is_a?(Hash)
    errors << "#{path}: top-level document must be a mapping"
    next
  end
  if File.basename(path).start_with?('config.')
    if doc.key?('contact_links') && !doc['contact_links'].is_a?(Array)
      errors << "#{path}: contact_links must be an array"
    end
    next
  end
  %w[name description body].each do |key|
    errors << "#{path}: missing #{key}" unless doc.key?(key)
  end
  validate_form_body.call(path, doc['body'], require_non_markdown: true)
end

Dir['.github/DISCUSSION_TEMPLATE/*.{yml,yaml}'].sort.each do |path|
  doc = parse.call(path)
  next if doc.nil?
  unless doc.is_a?(Hash)
    errors << "#{path}: top-level document must be a mapping"
    next
  end
  validate_form_body.call(path, doc['body'], require_non_markdown: true)
end

pr_template = '.github/pull_request_template.md'
if !File.exist?(pr_template) || File.read(pr_template).strip.empty?
  errors << "#{pr_template}: file is missing or empty"
end

config_path = '.github/ISSUE_TEMPLATE/config.yml'
if File.exist?(config_path)
  doc = parse.call(config_path)
  if doc.is_a?(Hash) && doc.key?('contact_links')
    links = doc['contact_links']
    unless links.is_a?(Array) && !links.empty?
      errors << "#{config_path}: contact_links must be a non-empty array when present"
    end
  end
end

funding_path = '.github/FUNDING.yml'
if File.exist?(funding_path)
  doc = parse.call(funding_path)
  unless doc.is_a?(Hash)
    errors << "#{funding_path}: top-level document must be a mapping"
  else
    custom = doc['custom']
    unless custom.is_a?(Array) && !custom.empty?
      errors << "#{funding_path}: custom must be a non-empty array"
    else
      custom.each_with_index do |value, idx|
        unless value.to_s.match?(%r{\Ahttps://agentram\.github\.io/ovpn/})
          errors << "#{funding_path}: custom[#{idx}] must point to the project Pages site"
        end
      end
    end
  end
end

if errors.empty?
  puts 'github template validation passed'
else
  warn errors.join("\n")
  exit 1
end
RUBY
