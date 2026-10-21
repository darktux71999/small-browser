//! Arena DOM built from the system lexbor HTML5 parser.
//! Lexbor owns parsing (spec-compliant tree construction); we own the mutable
//! tree so layout and JS can hold stable ids.

use std::collections::HashMap;

use crate::sys;

pub type NodeId = usize;

#[derive(Debug, Clone, Default)]
pub struct Node {
    pub tag: String,
    pub attrs: HashMap<String, String>,
    pub text: String,
    pub parent: Option<NodeId>,
    pub children: Vec<NodeId>,
    pub is_text: bool,
}

impl Node {
    pub fn attr(&self, name: &str) -> Option<&str> {
        self.attrs.get(name).map(|s| s.as_str())
    }
}

pub struct Document {
    pub nodes: Vec<Node>,
    pub root: NodeId,
}

impl Document {
    pub fn parse(html: &str) -> Document {
        unsafe {
            let doc = sys::lxb_html_document_create();
            assert!(!doc.is_null(), "lexbor: document create failed");
            let status = sys::lxb_html_document_parse(doc, html.as_ptr(), html.len());
            assert_eq!(status, 0, "lexbor: HTML parse failed ({status})");

            let node = doc as *mut sys::lxb_dom_node_t;
            let mut out = Document { nodes: Vec::new(), root: 0 };
            out.root = out.push("#document", false, None, HashMap::new(), String::new());
            out.walk_children(node, out.root);

            sys::lxb_html_document_destroy(doc);
            out
        }
    }

    fn push(
        &mut self,
        tag: &str,
        is_text: bool,
        parent: Option<NodeId>,
        attrs: HashMap<String, String>,
        text: String,
    ) -> NodeId {
        let id = self.nodes.len();
        self.nodes.push(Node {
            tag: tag.to_string(),
            attrs,
            text,
            parent,
            children: Vec::new(),
            is_text,
        });
        if let Some(p) = parent {
            self.nodes[p].children.push(id);
        }
        id
    }

    unsafe fn walk_children(&mut self, parent_ptr: *mut sys::lxb_dom_node_t, parent: NodeId) {
        let mut child = (*parent_ptr).first_child;
        while !child.is_null() {
            let node_type = (*child).node_type;
            match node_type {
                sys::LXB_DOM_NODE_TYPE_ELEMENT => {
                    let element = child as *mut sys::lxb_dom_element_t;
                    let mut len = 0usize;
                    let name_ptr = sys::lxb_dom_element_local_name(element, &mut len);
                    let tag = cstr(name_ptr, len);
                    let attrs = read_attrs(element);
                    let id = self.push(&tag, false, Some(parent), attrs, String::new());
                    self.walk_children(child, id);
                }
                sys::LXB_DOM_NODE_TYPE_TEXT => {
                    let mut len = 0usize;
                    let text_ptr = sys::lxb_dom_node_text_content(child, &mut len);
                    let text = cstr(text_ptr, len);
                    self.push("#text", true, Some(parent), HashMap::new(), text);
                }
                _ => {} // comments, doctype: not needed by the MVP tree
            }
            child = (*child).next;
        }
    }

    pub fn text_content(&self, id: NodeId) -> String {
        let mut out = String::new();
        self.collect_text(id, &mut out);
        out
    }

    fn collect_text(&self, id: NodeId, out: &mut String) {
        let node = &self.nodes[id];
        if node.is_text {
            out.push_str(&node.text);
        }
        for &c in &node.children {
            self.collect_text(c, out);
        }
    }

    /// MVP selectors: `tag`, `#id`, `.class`, `tag.class`, and descendant
    /// chains separated by spaces (`div .foo`).
    pub fn query_selector_all(&self, selector: &str) -> Vec<NodeId> {
        let chain = parse_selector(selector);
        if chain.is_empty() {
            return Vec::new();
        }
        (0..self.nodes.len())
            .filter(|&id| !self.nodes[id].is_text && self.matches_chain(id, &chain))
            .collect()
    }

    pub fn query_selector(&self, selector: &str) -> Option<NodeId> {
        self.query_selector_all(selector).into_iter().next()
    }

    fn matches_chain(&self, id: NodeId, chain: &[Compound]) -> bool {
        let Some(last) = chain.last() else { return false };
        if !self.matches_compound(id, last) {
            return false;
        }
        let mut ancestor = self.nodes[id].parent;
        for compound in chain.iter().rev().skip(1) {
            loop {
                let Some(a) = ancestor else { return false };
                if self.matches_compound(a, compound) {
                    ancestor = self.nodes[a].parent;
                    break;
                }
                ancestor = self.nodes[a].parent;
            }
        }
        true
    }

    fn matches_compound(&self, id: NodeId, c: &Compound) -> bool {
        let node = &self.nodes[id];
        if node.is_text {
            return false;
        }
        if let Some(tag) = &c.tag {
            if !node.tag.eq_ignore_ascii_case(tag) {
                return false;
            }
        }
        if let Some(attr) = &c.id {
            if node.attr("id") != Some(attr.as_str()) {
                return false;
            }
        }
        for class in &c.classes {
            let has = node
                .attr("class")
                .map(|v| v.split_whitespace().any(|x| x == class))
                .unwrap_or(false);
            if !has {
                return false;
            }
        }
        true
    }
}

#[derive(Debug, Default)]
struct Compound {
    tag: Option<String>,
    id: Option<String>,
    classes: Vec<String>,
}

fn parse_selector(selector: &str) -> Vec<Compound> {
    selector
        .split_whitespace()
        .filter(|s| *s != ">" && !s.is_empty())
        .map(|part| {
            let mut c = Compound::default();
            let mut rest = part;
            if let Some((tag, tail)) = split_tag(rest) {
                if !tag.is_empty() {
                    c.tag = Some(tag.to_string());
                }
                rest = tail;
            }
            while !rest.is_empty() {
                let (kind, tail) = rest.split_at(1);
                let end = tail
                    .find(|ch| ch == '#' || ch == '.')
                    .unwrap_or(tail.len());
                let (name, tail) = tail.split_at(end);
                match kind {
                    "#" => c.id = Some(name.to_string()),
                    "." => c.classes.push(name.to_string()),
                    _ => {}
                }
                rest = tail;
            }
            c
        })
        .collect()
}

fn split_tag(s: &str) -> Option<(&str, &str)> {
    let end = s.find(|c| c == '#' || c == '.').unwrap_or(s.len());
    Some((&s[..end], &s[end..]))
}

unsafe fn read_attrs(element: *mut sys::lxb_dom_element_t) -> HashMap<String, String> {
    let mut out = HashMap::new();
    let mut attr = (*element).first_attr;
    while !attr.is_null() {
        let mut name_len = 0usize;
        let name_ptr = sys::lxb_dom_attr_local_name(attr, &mut name_len);
        let name = cstr(name_ptr, name_len);
        let value = if (*attr).value.is_null() {
            String::new()
        } else {
            let s = &*(*attr).value;
            cstr(s.data, s.length)
        };
        out.insert(name, value);
        attr = (*attr).next;
    }
    out
}

unsafe fn cstr(ptr: *const u8, len: usize) -> String {
    if ptr.is_null() || len == 0 {
        return String::new();
    }
    String::from_utf8_lossy(std::slice::from_raw_parts(ptr, len)).into_owned()
}
