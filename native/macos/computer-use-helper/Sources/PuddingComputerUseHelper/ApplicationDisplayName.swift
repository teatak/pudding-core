import Foundation

enum ApplicationDisplayName {
  // Read the target bundle, not the Helper's language or a translated app-ID map.
  // macOS apps ship either InfoPlist.loctable or <language>.lproj/InfoPlist.strings.
  static func resolve(bundle: Bundle?, url: URL, preferredLanguages: [String]) -> String {
    var localized: [String: Any] = [:]
    if let bundle, let resources = bundle.resourceURL,
      let language = Bundle.preferredLocalizations(
        from: bundle.localizations, forPreferences: preferredLanguages
      ).first
    {
      let table = dictionary(at: resources.appendingPathComponent("InfoPlist.loctable"))
      localized = (table[language] as? [String: Any])
        ?? dictionary(at: resources.appendingPathComponent("\(language).lproj/InfoPlist.strings"))
    }
    for info in [localized, bundle?.infoDictionary ?? [:]] {
      for key in ["CFBundleDisplayName", "CFBundleName"] {
        if let name = info[key] as? String, !name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
          return name
        }
      }
    }
    return url.deletingPathExtension().lastPathComponent
  }

  private static func dictionary(at url: URL) -> [String: Any] {
    guard let data = try? Data(contentsOf: url),
      let value = try? PropertyListSerialization.propertyList(from: data, format: nil)
    else { return [:] }
    return value as? [String: Any] ?? [:]
  }
}
